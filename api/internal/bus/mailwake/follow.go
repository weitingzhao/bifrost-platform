package mailwake

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Follow wakes the waiters of this process on subject (core NATS, like the
// gate's waiters: nothing is kept; a missed event is covered by the waiters'
// slow re-read). A receipt is left only when a waiter was woken.
func (h *Hub) Follow(ctx context.Context, nc *nats.Conn, receipts *relay.Receipts, subject string) error {
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) {
		id := h.Take(m.Data)
		if id == "" {
			return
		}
		readerseen.Saw(Consumer)
		safego.Go("bus-mail-receipt", func() {
			rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := receipts.Publish(rctx, id, Consumer); err != nil {
				slog.Warn("bus: inbox waiters' receipt", "event", id, "error", err)
			}
		})
	})
	if err != nil {
		return err
	}
	context.AfterFunc(ctx, func() { _ = sub.Unsubscribe() })
	return nil
}
