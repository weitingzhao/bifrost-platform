package relay

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
)

// EventsAfterOf is the event log's cross-stream read under filter, its Types
// being the ones each read asks for.
func EventsAfterOf(filter eventlog.EventFilter) EventsAfterFunc {
	return func(ctx context.Context, tx pgx.Tx, after Cursor, limit int, types []string) ([]byte, Cursor, error) {
		f := filter
		f.Types = types
		doc, next, err := eventlog.EventsAfter(ctx, tx, eventlog.EventCursor{TxID: after.TxID, EventID: after.EventID}, limit, f)
		return doc, Cursor{TxID: next.TxID, EventID: next.EventID}, err
	}
}

// CursorAt is the gate's since=: the ledger's read-on position from about a
// time (eventlog.CursorAt), read in a read-only transaction.
func CursorAt(db TxBeginner) func(context.Context, time.Time) (Cursor, error) {
	return func(ctx context.Context, t time.Time) (Cursor, error) {
		tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
		if err != nil {
			return Cursor{}, err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
		c, err := eventlog.CursorAt(ctx, tx, t)
		return Cursor{TxID: c.TxID, EventID: c.EventID}, err
	}
}

// EnsureEventStreamUntil ensures the events stream, again every 15 seconds
// until it can; false when ctx ended first.
func EnsureEventStreamUntil(ctx context.Context, js jetstream.JetStream, tenant, env string) bool {
	for {
		ectx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := EnsureEventStream(ectx, js, tenant, env)
		cancel()
		if err == nil {
			return true
		}
		slog.Error("bus relay: the events stream could not be ensured; again in 15s", "error", err)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(15 * time.Second):
		}
	}
}

// ConsumePasses runs pass for each event of a durable consumer and leaves a
// receipt: the event says "look now", pass reads the source.
func ConsumePasses(ctx context.Context, js jetstream.JetStream, env, name string, types []string, pass func(context.Context)) {
	receipts := &Receipts{JS: js, Tenant: Tenant, Env: env}
	cc, err := ConsumeDurable(ctx, js, Tenant, env, name, types, func(e Envelope) error {
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		pass(dctx)
		readerseen.Saw(name)
		if err := receipts.Publish(dctx, e.ID, name); err != nil {
			slog.Warn("bus: pass receipt", "consumer", name, "event", e.ID, "error", err)
		}
		return nil
	})
	if err != nil {
		slog.Error("bus: pass consumer", "consumer", name, "error", err)
		return
	}
	context.AfterFunc(ctx, func() { cc.Stop() })
	slog.Info("bus: passes run on events", "consumer", name, "on", types)
}
