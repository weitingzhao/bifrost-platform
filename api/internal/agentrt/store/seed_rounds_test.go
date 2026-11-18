package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Scenes of the retired dispatch lane, written as its rows (store cut 3b-2,
// 2026-11-14). Nothing leases, runs or checks a task any more, but the plan's
// bindings, the artifact search and the progress read still read the rows a
// round left: the tasks, their attempts and the verifications. These
// fixtures write those rows as an import does (a legacy_imported event of
// the transaction), in the shape the lane left them.

// seedHost makes host id exist: an attempt names the host it ran on.
func seedHost(t *testing.T, tx pgx.Tx, id string) {
	t.Helper()
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name)
 VALUES ('host:' || $1, 'host', 'Agent host ' || $1) ON CONFLICT DO NOTHING`, id)
	ok(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO agentrt.host (host_id, actor_id, host_kind, state)
 VALUES ($1, 'host:' || $1, 'dev', 'active') ON CONFLICT DO NOTHING`, id)
	ok(t, err)
}

// seeded runs fn in a transaction that carries an import event, as a
// fixture's rows need.
func (w *world) seeded(t *testing.T, fn func(tx pgx.Tx)) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test"}}); err != nil {
			return err
		}
		fn(tx)
		return nil
	}))
}
