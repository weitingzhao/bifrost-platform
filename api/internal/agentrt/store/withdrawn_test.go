package store_test

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// newWorldOf is newKernelWorld with only the given tools as the jobs'
// candidates, so a test knows every executor a task's route has.
func newWorldOf(t *testing.T, tools ...store.Tool) *world {
	t.Helper()
	w := newWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test") }))
	for i := 1; i <= 3; i++ {
		ok(t, store.RegisterHost(ctx, w.rt, "owner", fmt.Sprintf("h%d", i), "dev"))
	}
	_, err := w.rt.Exec(ctx, agentrttest.DispatchRules)
	ok(t, err)
	w.legacy(t, agentrttest.OwnerFactsRule, agentrttest.BindOwnerFacts)
	w.jobs(t, "active", "*", tools...)
	return w
}

// blockReason is the reason of the task's latest move to blocked.
func (w *world) blockReason(t *testing.T, task string) string {
	t.Helper()
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE task_id = $1 AND event_type = 'task_state_changed'
	  AND body_json->>'to' = 'blocked' ORDER BY event_id DESC LIMIT 1`, task).Scan(&reason))
	return reason
}
