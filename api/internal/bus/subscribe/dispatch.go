package subscribe

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// RosterClients is every tenant's declared users expected in env, and each
// dispatcher row's consumer (declared by its row; no lost_after: a row that
// only fires on red verdicts is quiet most days).
func RosterClients(subs map[string]registry.Subscriptions, env string) []registry.Client {
	var out []registry.Client
	names := make([]string, 0, len(subs))
	for n := range subs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		for _, c := range subs[n].Clients {
			if c.InEnv(env) {
				out = append(out, c)
			}
		}
	}
	for _, c := range Consumers(subs, env) {
		out = append(out, registry.Client{Name: c.Name(relay.Tenant), Kind: "durable", Note: "subscription row " + c.Tenant + "/" + c.Row.ID})
	}
	return out
}

// Dispatcher is what the subscription tables' rows are delivered with.
type Dispatcher struct {
	DB     eventlog.DB
	Read   CommitMessage // nil without a delivery service
	Detail Detail
	Send   Sender
	// Known, when set, says who can receive a wake instead of asking the
	// ledger's actors (tests).
	Known Known
}

// notifyPush is a notify row's push: the same relay as the approvals' own
// (notice.Send); the relay's answer per target is logged, the evidence that
// the bus's path reached the phone (2026-11-17: the old direct push stays
// until it has, several times).
func notifyPush(ctx context.Context, key, title, body, click string) error {
	ds, err := notice.Send(ctx, notice.Message{Title: title, Message: body, ClickURL: click, Priority: 4, Fallback: "Bus notice", Kind: "bus", ID: key})
	for _, d := range ds {
		slog.Info("bus: notify", "key", key, "channel", d.Channel, "target", d.Target, "result", d.Result, "error", d.Error)
	}
	return err
}

// knownThread says whether addr is a registered thread.
func (d Dispatcher) knownThread(ctx context.Context, addr string) (bool, error) {
	var kind string
	err := eventlog.InTx(ctx, d.DB, func(tx pgx.Tx) error {
		k, err := actor.ActorKind(ctx, tx, addr)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		kind = k
		return err
	})
	return kind == "thread", err
}

// Run is the tables' dispatcher: one durable consumer per row it delivers,
// each event handed to its row, a receipt left under the row's name. Rows
// other code still delivers (by: ...) are not consumed here.
func (d Dispatcher) Run(ctx context.Context, js jetstream.JetStream, env string) {
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		slog.Error("bus: subscription tables do not load; no row is dispatched", "error", err)
		return
	}
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	want := Consumers(subs, env)
	// A row or a tenant that left takes its consumer with it.
	lctx, lcancel := context.WithTimeout(ctx, 10*time.Second)
	if names, err := relay.DurableNames(lctx, js, relay.Tenant, env); err != nil {
		slog.Warn("bus: subscription consumers not listed; none removed", "error", err)
	} else {
		for _, n := range Stale(names, want, relay.Tenant) {
			if err := relay.DeleteDurable(lctx, js, relay.Tenant, env, n); err != nil {
				slog.Warn("bus: stale subscription consumer not removed", "consumer", n, "error", err)
				continue
			}
			slog.Info("bus: subscription consumer removed, its row is gone", "consumer", n)
		}
	}
	lcancel()
	for _, c := range want {
		row, name := c.Row, c.Name(relay.Tenant)
		cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, name, row.On, func(e relay.Envelope) error {
			dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			var out Outcome
			var err error
			if row.Deliver == registry.DeliverNotify {
				out, err = Notify(dctx, row, FromEnvelope(e), notifyPush)
			} else {
				out, err = Deliver(dctx, row, FromEnvelope(e), d.Read, d.Detail, d.Send, d.knownThread, UndeliverableTo(subs, c.Tenant))
			}
			if err != nil {
				slog.Warn("bus: subscription delivery; asked again", "tenant", c.Tenant, "row", row.ID, "event", e.ID, "error", err)
				return err
			}
			if out == Rerouted || out == Undeliverable {
				slog.Warn("bus: subscription wake had no registered recipient", "tenant", c.Tenant, "row", row.ID, "event", e.ID, "outcome", out)
			}
			if err := receipts.Publish(dctx, e.ID, name); err != nil {
				slog.Warn("bus: subscription receipt", "row", row.ID, "event", e.ID, "error", err)
			}
			return nil
		})
		if err != nil {
			slog.Error("bus: subscription consumer", "tenant", c.Tenant, "row", row.ID, "error", err)
			continue
		}
		context.AfterFunc(ctx, func() { cc.Stop() })
		slog.Info("bus: subscription dispatched", "tenant", c.Tenant, "row", row.ID, "consumer", name, "on", row.On, "deliver", row.Deliver)
	}
}

// DeliverDirect delivers ev to the tenant's rows marked by: webhook, now, in
// the caller's request (2026-11-20, TD-588): the alert webhook calls it for
// each alert it wrote, so an alarm about the bus reaches its seat while the
// bus is down. Those rows have no durable consumer; the bus never delivers
// them a second time. Each row's outcome is returned; an error stops nothing
// else (the alert is already in the ledger).
func (d Dispatcher) DeliverDirect(ctx context.Context, subs map[string]registry.Subscriptions, tenant string, ev Event) []error {
	var errs []error
	known := Known(d.knownThread)
	if d.Known != nil {
		known = d.Known
	}
	for _, row := range subs[tenant].Rows {
		if row.By != registry.ByWebhook {
			continue
		}
		out, err := Deliver(ctx, row, ev, d.Read, d.Detail, d.Send, known, UndeliverableTo(subs, tenant))
		if err != nil {
			errs = append(errs, err)
			slog.Warn("bus: direct delivery failed", "row", row.ID, "event", ev.ID, "error", err)
			continue
		}
		if out != Skipped {
			slog.Info("bus: delivered without the bus", "row", row.ID, "event", ev.ID, "outcome", out)
		}
	}
	return errs
}
