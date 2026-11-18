package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	busalerts "github.com/weitingzhao/bifrost-platform/api/internal/bus/alerts"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/subscribe"
)

const (
	alertLettersConsumer = "alert-letters"
	alertLettersRow      = "alert-firing-to-duty"
)

// consumeAlertLetters writes the duty seat its alert letters: the row in the
// subscription table says who gets them, bus/alerts which firings share one.
func (s *Server) consumeAlertLetters(ctx context.Context, js jetstream.JetStream, env string) {
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		slog.Error("bus: alert letters: the subscription table does not load", "error", err)
		return
	}
	var row registry.Subscription
	for _, r := range subs[relay.Tenant].Rows {
		if r.ID == alertLettersRow {
			row = r
		}
	}
	if row.ID == "" {
		slog.Error("bus: alert letters: no row", "row", alertLettersRow)
		return
	}
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, alertLettersConsumer, row.On, func(e relay.Envelope) error {
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := s.writeAlertLetter(dctx, row, e); err != nil {
			slog.Warn("bus: alert letter; asked again", "event", e.ID, "error", err)
			return err
		}
		readerseen.Saw(alertLettersConsumer)
		if err := receipts.Publish(dctx, e.ID, alertLettersConsumer); err != nil {
			slog.Warn("bus: alert letter receipt", "event", e.ID, "error", err)
		}
		return nil
	})
	if err != nil {
		slog.Error("bus: alert letters consumer", "error", err)
		return
	}
	context.AfterFunc(ctx, func() { cc.Stop() })
}

func (s *Server) writeAlertLetter(ctx context.Context, row registry.Subscription, e relay.Envelope) error {
	ev := subscribe.FromEnvelope(e)
	db := s.bus.db()
	n, ok := relay.ParseEventID(e.ID)
	if db == nil || !ok || !subscribe.Matches(row, ev) {
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, e.Time)
	if err != nil {
		at = time.Now()
	}
	key, err := busalerts.RuleOf(row.ID, row.Params["always_namespaces"]).LetterKeyOf(ctx, db, e.ID, n, at)
	if err != nil || key == "" {
		return err
	}
	return coord.PostConclusionCaused(ctx, db, key, row.To, subscribe.Body(row, ev), n)
}
