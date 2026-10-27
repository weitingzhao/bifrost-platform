package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock via acceptedDrill. The drill lists only tasks it
// could claim: one accepted before the landing was enabled, or whose mission
// is not active, would be refused after a Git read inside a held transaction
// (2026-10-27 STG: twelve pre-enable tasks held one of the pool's two
// connections all the time and stalled the other loops).
func TestLandingCandidatesLeaveOutWhatOnlyTheDatabaseRefuses(t *testing.T) {
	w, task, _ := acceptedDrill(t)
	enabledAt := landingState(t, w, landingRepoStream).EnabledAt
	if enabledAt.IsZero() {
		t.Fatal("no enable time")
	}
	list := func(at time.Time) []string {
		t.Helper()
		tasks, e := store.LandingCandidates(ctx, w.rt, at)
		ok(t, e)
		return tasks
	}
	if got := list(enabledAt); len(got) != 1 || got[0] != task {
		t.Fatal("eligible task not listed", got)
	}
	if got := list(time.Time{}); len(got) != 0 {
		t.Fatal("listed without an enable time", got)
	}
	// Enabled after the task was accepted and its mission created: before_enable.
	if got := list(time.Now().Add(time.Hour)); len(got) != 0 {
		t.Fatal("listed a task accepted before the landing was enabled", got)
	}
	var mission string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&mission))
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveMission(ctx, tx, "controller", mission, "stopped", "test")
	}))
	if got := list(enabledAt); len(got) != 0 {
		t.Fatal("listed a task of a mission that is not active", got)
	}
}
