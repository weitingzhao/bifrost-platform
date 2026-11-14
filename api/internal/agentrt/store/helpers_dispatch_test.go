package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Fixtures many tests of this package build their scene with. They lived in
// close_test.go and prune_test.go, which went with the code they tested
// (LANE-BIZ-ALIGN store cut 1, 2026-11-14); the fixtures stay.

// accept moves a task through to accepted, as the kernel and acceptance do.
func (w *world) accept(t *testing.T, id string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, to := range []string{"leased", "running", "submitted", "accepted"} {
			if err := store.MoveTaskForTest(ctx, tx, "controller", id, to); err != nil {
				return err
			}
		}
		return nil
	}))
}

// goalMission is an active mission bound to no plan step, as a goal makes.
func (w *world) goalMission(t *testing.T, id string) {
	t.Helper()
	ctx := ctxOf(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: id, Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", id, "active", "test")
	}))
}
