package migrate_test

import (
	"context"
	"time"

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

// seedLease writes task's attempt no as leased to executor on host, in the
// caller's transaction.
func seedLease(ctx context.Context, tx pgx.Tx, actor, mission, task, executor, host string, no int, ttl time.Duration) (string, int64, error) {
	if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
		Body: map[string]any{"source": "test"}}); err != nil {
		return "", 0, err
	}
	id := store.NewAttemptID()
	var epoch int64
	err := tx.QueryRow(ctx, `INSERT INTO agentrt.attempt
 (attempt_id, task_id, attempt_no, executor_actor_id, host_id, lease_epoch, lease_expires_at, heartbeat_at, state)
 SELECT $1, $2, $3, $4, NULLIF($5, ''), COALESCE((SELECT max(lease_epoch) FROM agentrt.attempt), 0) + 1,
  clock_timestamp() + make_interval(secs => $6), clock_timestamp(), 'leased' RETURNING lease_epoch`,
		id, task, no, executor, host, ttl.Seconds()).Scan(&epoch)
	return id, epoch, err
}
