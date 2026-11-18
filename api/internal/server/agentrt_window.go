package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// The 15-second observation of window expiry (L24 of the bus's channel
// table). It used to write each window decision as a note on the stream
// release-window:<ns>; that stream had no reader and release.window.changed
// (gate-cluster) announces the window now, so the note was removed
// (2026-11-17, with the communication line). The observation itself stays:
// the queue notices still follow it.
func (s *Server) followWindowExpiry(ctx context.Context) {
	safego.Go("release-window-expiry", func() {
		ticker := time.NewTicker(time.Second * 15)
		defer ticker.Stop()
		for {
			if s.delivery != nil {
				readCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				if err := s.delivery.ObserveWindowExpiry(readCtx); err != nil {
					slog.Warn("observe release window expiry", "err", err)
				}
				cancel()
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
}
