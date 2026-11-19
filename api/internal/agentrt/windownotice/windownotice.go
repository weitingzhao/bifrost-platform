// Package windownotice posts the release window's queue notices to the seats
// waiting in it.
package windownotice

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

// Post posts a release-window queue notice (delivery/window_notify.go).
// The notices are facts, not decisions: delivery dedups by key in its own
// process (24 h), and after a platform restart one notice may be sent again,
// which is harmless. So this posts through the public message path only.
func Post(ctx context.Context, db eventlog.DB, to, body string) error {
	if to == "" || threadinbox.Recipient(to) != to {
		return nil
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		kind, err := actor.ActorKind(ctx, tx, to)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && kind != "thread" {
			return nil // never register or revive a recipient on someone else's behalf
		}
		if err != nil {
			return err
		}
		_, err = coord.PostMessage(ctx, tx, "controller", to, "note", []byte(body))
		return err
	})
}
