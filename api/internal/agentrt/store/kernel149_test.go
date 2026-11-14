package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Regression tests for review 149.

// A1: a plan change that moves a candidate to another mission between the
// claim's read and its lock makes the claim skip it.
//
// Serial: it holds the package's test hook (SetHook).
func TestClaimSkipsATaskMovedToAnotherMission(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	task := w.ready(t, store.Task{})
	moved := false
	unhook := store.SetHook(func(point, arg string) {
		if point != "picked" || arg != task || moved {
			return
		}
		moved = true
		ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
				Body: map[string]any{"change": "move the task to M-2"}}); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2, mission_id = 'M-2' WHERE task_id = $1`, task, s2)
			return err
		}))
	})
	c := w.claim(t, "h1", store.LeaseTTL, claude)
	unhook()
	var mission string
	var attempts int
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id, (SELECT count(*) FROM agentrt.attempt WHERE task_id = $1) FROM agentrt.task WHERE task_id = $1`,
		task).Scan(&mission, &attempts))
	if !moved || c != nil || mission != "M-2" || attempts != 0 || w.state(t, "task", task) != "ready" {
		t.Fatalf("moved %v, claim %+v, mission %s, %d attempts", moved, c, mission, attempts)
	}
}
