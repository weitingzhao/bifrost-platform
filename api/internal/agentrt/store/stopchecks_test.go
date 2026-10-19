package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// T09: a stop on a task under verification stops its check too: the check
// is not claimed while the stop holds, and a check already running is told to
// stop at its next heartbeat. Lifted, the check goes on.
func TestAStopReachesTheTasksChecks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	first := store.NewControlID()
	w.control(t, first, "task", impl, "stop")
	if c := w.claim(t, "h2", store.LeaseTTL, deterministic); c != nil {
		t.Fatalf("the check of a stopped task was claimed: %+v", c)
	}
	w.clear(t, first)
	c := w.claim(t, "h2", store.LeaseTTL, deterministic)
	if c == nil || c.TaskID != v {
		t.Fatalf("the check after the lift = %+v", c)
	}
	f := fence(c, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	if b, err := store.Heartbeat(ctx, w.rt, f); err != nil || b.Stop {
		t.Fatalf("a heartbeat before the stop: %+v %v", b, err)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.SetControl(ctx, tx, "owner", store.NewControlID(), "task", impl, "stop", "test")
	}))
	if b, err := store.Heartbeat(ctx, w.rt, f); err != nil || !b.Stop {
		t.Fatalf("the running check's heartbeat after its subject's stop: %+v %v", b, err)
	}
}

// Review 341 A3: a check that was submitted before its subject was stopped
// is not used while the stop holds, whatever its role and its verdict: no
// verification is recorded, the subject keeps its state and its round, and
// no new check is made. Lifted, the result is used as it would have been.
func TestAStoppedTasksChecksWait(t *testing.T) {
	t.Parallel()
	type seen struct {
		state         string
		round, checks int
	}
	look := func(w *world, impl string) seen {
		var s seen
		s.state = w.state(t, "task", impl)
		ok(t, w.rt.QueryRow(ctx, `SELECT round_no FROM agentrt.task WHERE task_id = $1`, impl).Scan(&s.round))
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE subject_task_id = $1`, impl).Scan(&s.checks))
		return s
	}
	stopped := func(w *world, impl, check, then string) {
		t.Helper()
		before, recorded := look(w, impl), len(w.verifications(t, impl))
		id := store.NewControlID()
		w.control(t, id, "task", impl, "stop")
		if did, err := store.Conclude(ctx, w.rt, check); err != nil || did != "" {
			t.Fatalf("a check of a stopped task was used: %q (%v)", did, err)
		}
		if after := look(w, impl); after != before || len(w.verifications(t, impl)) != recorded || w.state(t, "task", check) != "submitted" {
			t.Fatalf("while stopped: %+v, was %+v; check %s", after, before, w.state(t, "task", check))
		}
		w.clear(t, id)
		if did, err := store.Conclude(ctx, w.rt, check); err != nil || did != then {
			t.Fatalf("after the lift the check did %q (%v), want %q", did, err, then)
		}
	}

	// A verification that failed: lifted, the task goes round again.
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "failed")
	stopped(w, impl, v, "rework")

	// A review that passed: lifted, the task is accepted.
	w = newKernelWorld(t)
	impl = w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	v, err = store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "done")
	if did, err := store.Conclude(ctx, w.rt, v); err != nil || did != "review" {
		t.Fatalf("verification: %q (%v)", did, err)
	}
	review := w.checker(t, impl, "review")
	w.run(t, "h3", codex, "done")
	stopped(w, impl, review, "accepted")
}
