package store_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Regression tests for review 150.

// B1: a review claim passes over a subject someone holds instead of waiting
// for it, so a plan change that moves the subject and then the review never
// closes a cycle with the claim.
func TestReviewClaimDoesNotWaitForItsSubject(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	subject := store.NewTaskID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: subject, MissionID: "M-1", PlanNode: s1, Role: "implement",
			Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "branch"})
		return err
	}))
	review := w.ready(t, store.Task{Role: "review", Subject: subject, Effect: "none"})

	move, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = move.Rollback(ctx) }()
	_, err = store.Append(ctx, move, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
		Body: map[string]any{"change": "move a subject and its review"}})
	ok(t, err)
	_, err = move.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2 WHERE task_id = $1`, subject, s3)
	ok(t, err)

	done := make(chan claimed, 1)
	go func() {
		c, claimErr := store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{claude}})
		done <- claimed{c, claimErr}
	}()
	var got *claimed
	select {
	case g := <-done:
		got = &g
	case <-time.After(3 * time.Second):
	}
	_, err = move.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2 WHERE task_id = $1`, review, s3)
	ok(t, err)
	ok(t, move.Commit(ctx))
	if got == nil {
		g := <-done
		got = &g
	}
	ok(t, got.err)
	var attempts int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.attempt WHERE task_id = $1`, review).Scan(&attempts))
	if got.c != nil || attempts != 0 || w.state(t, "task", review) != "ready" {
		t.Fatalf("claim %+v, %d attempts", got.c, attempts)
	}
}

// B2: attempts a writer holds do not hide the ones after them from the
// sweep.
func TestSweepReadsPastHeldAttempts(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	lease := func(ago time.Duration) string {
		var id string
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			task := store.NewTaskID()
			if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-1", PlanNode: s1, Role: "implement",
				Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "branch"}); err != nil {
				return err
			}
			var err error
			id, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:codex", "", 1, -ago)
			return err
		}))
		return id
	}
	var held []string
	for i := 0; i < 100; i++ {
		held = append(held, lease(2*time.Minute))
	}
	late := lease(time.Minute)
	holder, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, `SELECT 1 FROM agentrt.attempt WHERE attempt_id = ANY($1) FOR UPDATE`, held)
	ok(t, err)
	stale, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if stale != 1 || w.state(t, "attempt", late) != "stale" {
		t.Fatalf("the sweep moved %d with the first 100 held", stale)
	}
}
