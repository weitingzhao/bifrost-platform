package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

// Release-window queue notices (delivery/window_notify.go). The notices are
// facts, not decisions: delivery dedups by key in its own process (24 h), and
// after a platform restart one notice may be sent again, which is harmless.
// So this posts through the store's public message path only: it writes no
// store table of its own and takes no store lock (agentrt/store is another
// seat's, and a durable once-per-key table here would duplicate
// store.PostConclusion's internals).
func (s *Server) wireWindowNotices(db store.DB) {
	if s.delivery == nil {
		return
	}
	s.delivery.SetWindowNotifier(func(ctx context.Context, _ /* key */, to, body string) error {
		return postWindowNotice(ctx, db, to, body)
	})
}

func postWindowNotice(ctx context.Context, db store.DB, to, body string) error {
	if to == "" || threadinbox.Recipient(to) != to {
		return nil
	}
	return store.InTx(ctx, db, func(tx pgx.Tx) error {
		kind, err := store.ActorKind(ctx, tx, to)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && kind != "thread" {
			return nil // never register or revive a recipient on someone else's behalf
		}
		if err != nil {
			return err
		}
		_, err = store.PostMessage(ctx, tx, "controller", to, "note", []byte(body))
		return err
	})
}
