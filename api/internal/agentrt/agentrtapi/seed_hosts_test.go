package agentrtapi_test

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Rows of the retired dispatch lane, written as it wrote them (store cut
// 3b-2, 2026-11-14): nothing registers a host or leases a task any more, and
// the tests here still need the rows to exist.

// seedHost registers host id of kind as the lane did: its event, its actor,
// its row.
func seedHost(ctx context.Context, db store.DB, by, id, kind string) error {
	return store.InTx(ctx, db, func(tx pgx.Tx) error {
		actor := "host:" + id
		if _, err := store.Append(ctx, tx, store.Event{Stream: "host:" + id, Type: "host_registered", Actor: by,
			Body: map[string]any{"host_id": id, "actor_id": actor, "host_kind": kind}}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ($1, 'host', $2)`,
			actor, "Agent host "+id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state) VALUES ($1, $2, $3, 'active')`, id, actor, kind)
		return err
	})
}
