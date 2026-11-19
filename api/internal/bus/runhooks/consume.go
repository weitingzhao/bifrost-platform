package runhooks

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// Consume starts the hooks' durable consumers on finishedType, leaving a
// receipt under each hook's consumer name.
func (r *Registry) Consume(ctx context.Context, js jetstream.JetStream, env, finishedType string) {
	receipts := &relay.Receipts{JS: js, Tenant: relay.Tenant, Env: env}
	for _, err := range r.Start(func(h Hook) error {
		cc, err := relay.ConsumeDurable(ctx, js, relay.Tenant, env, h.Consumer(), []string{finishedType}, func(e relay.Envelope) error {
			dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := Deliver(dctx, h, e.Subject); err != nil {
				slog.Warn("bus: run hook; asked again", "hook", h.Name, "event", e.ID, "error", err)
				return err
			}
			if err := receipts.Publish(dctx, e.ID, h.Consumer()); err != nil {
				slog.Warn("bus: run hook receipt", "hook", h.Name, "event", e.ID, "error", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		context.AfterFunc(ctx, func() { cc.Stop() })
		slog.Info("bus: run hook woken by ci.run.finished", "hook", h.Name, "prefix", h.Prefix, "consumer", h.Consumer())
		return nil
	}) {
		slog.Error("bus: run hook consumer", "error", err)
	}
}
