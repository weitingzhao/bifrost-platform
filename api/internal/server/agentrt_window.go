package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Window events have their own stream, separate from B5's release protocol.
// Use the runtime's existing note envelope; its text is a versioned JSON record.
// This append is best effort across the Kubernetes/PostgreSQL boundary. A
// durable outbox is still needed to close the write-to-append crash gap.
func (s *Server) recordWindowEvent(ctx context.Context, event delivery.WindowEvent) {
	if s.agentRuntime == nil || s.agentRuntime.late == nil {
		return
	}
	raw, err := json.Marshal(struct {
		Version int                  `json:"version"`
		Window  delivery.WindowEvent `json:"release_window"`
	}{1, event})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err = appendWindowEvent(ctx, s.agentRuntime.late, event, string(raw))
	if err != nil {
		slog.Error("release window runtime event", "action", event.Action, "err", err)
	}
}

func appendWindowEvent(ctx context.Context, db interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, event delivery.WindowEvent, text string) error {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	stream := "release-window:" + event.Namespace
	// Serialize deduplication and Append in the same transaction and connection.
	// In particular this never borrows a second pool connection under a lock.
	if _, err = tx.Exec(ctx, `INSERT INTO agentrt.event_stream (event_stream_id) VALUES ($1) ON CONFLICT DO NOTHING`, stream); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id=$1 FOR UPDATE`, stream); err != nil {
		return err
	}
	if event.Action != "refused" {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id=$1 AND body_json->>'text'=$2)`, stream, text).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return tx.Commit(ctx)
		}
	}
	if _, err = store.Append(ctx, tx, store.Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": text}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

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
