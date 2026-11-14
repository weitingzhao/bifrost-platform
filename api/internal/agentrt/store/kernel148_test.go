package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Regression tests for review 148.

type claimed struct {
	c   *store.Claim
	err error
}

// A3: Start and Submit move the task, so they lock it before reading the
// clock; a wait for the task row that crosses the deadline is refused and
// leaves no event, no state change and no idempotency key. The submit is
// prepared while the start waits, so the two waits overlap (CI-16).
func TestTaskLockWaitCrossesTheDeadline(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	const taskLock = "FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE"
	hold := func(taskID string) pgx.Tx {
		tx, err := pool.Begin(ctx)
		ok(t, err)
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR UPDATE`, taskID)
		ok(t, err)
		return tx
	}

	// Six and ten seconds to the deadlines, for each request to reach the
	// lock it waits on whatever the tests beside this one do (review 322 A1).
	w.ready(t, store.Task{})
	c := w.claim(t, "h1", -store.Grace+6*time.Second, codex)
	startDeadline := c.LeaseExpiresAt.Add(store.Grace)
	startHolder := hold(c.TaskID)
	started := make(chan error, 1)
	go func() { started <- store.Start(ctx, pool, fence(c, "h1"), "", "") }()
	w.waitFor(t, taskLock, startDeadline)

	w.ready(t, store.Task{Effect: "none"})
	s := w.claim(t, "h2", -store.Grace+10*time.Second, codex)
	f := fence(s, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "# none\n")}, nil)
	ok(t, err)
	submitDeadline := s.LeaseExpiresAt.Add(store.Grace)
	submitHolder := hold(s.TaskID)
	submitted := make(chan error, 1)
	go func() {
		_, err := store.Submit(ctx, pool, f)
		submitted <- err
	}()
	// The start still waits (its holder commits below), so two waits.
	w.waitForN(t, taskLock, 2, submitDeadline)

	time.Sleep(time.Until(startDeadline) + time.Second)
	ok(t, startHolder.Commit(ctx))
	is(t, <-started, store.ErrFenced)
	if w.state(t, "attempt", c.AttemptID) != "leased" || w.events(t, "attempt_started", c.AttemptID) != 0 {
		t.Fatal("a refused start changed the attempt")
	}

	time.Sleep(time.Until(submitDeadline) + time.Second)
	ok(t, submitHolder.Commit(ctx))
	is(t, <-submitted, store.ErrFenced)
	var keys int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
		"host:h2|submit|"+s.AttemptID).Scan(&keys))
	if w.events(t, "attempt_submitted", s.AttemptID) != 0 || w.state(t, "task", s.TaskID) != "running" || keys != 0 {
		t.Fatalf("a refused submit left %d keys or changed state", keys)
	}
}

// B1: a candidate the claim looks at and does not take is unlocked before
// the claim goes on.
//
// Serial: it holds the package's test hook (SetHook).
func TestSkippedCandidateHoldsNothing(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	w.ready(t, store.Task{Role: "plan", Effect: "none"})
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c == nil {
		t.Fatal("the first plan task was not claimed")
	}
	busy := w.ready(t, store.Task{Role: "plan", Effect: "none"})
	open := w.ready(t, store.Task{})
	var probe error
	probed := false
	unhook := store.SetHook(func(point, arg string) {
		if point != "taken" || arg != open {
			return
		}
		probed = true
		tx, err := pool.Begin(ctx)
		if err != nil {
			probe = err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, probe = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR UPDATE NOWAIT`, busy)
	})
	c := w.claim(t, "h2", store.LeaseTTL, claude)
	unhook()
	if c == nil || c.TaskID != open || !probed {
		t.Fatalf("claim = %+v, probed %v", c, probed)
	}
	var pgErr *pgconn.PgError
	if errors.As(probe, &pgErr) {
		t.Fatalf("the skipped plan task was still locked: %v", probe)
	}
	ok(t, probe)
}

// B2: the claim reads past a full page of candidates it may not take.
func TestClaimLooksPastAFullPage(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	for i := 0; i < 100; i++ {
		w.ready(t, store.Task{Needs: []string{"git_commit"}})
	}
	last := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c == nil || c.TaskID != last {
		t.Fatalf("claim = %+v; want %s behind 100 tasks codex may not take", c, last)
	}
}

// B4: a review of core work is core work even when its proposer left Core
// out.
func TestReviewOfCoreWorkIsCore(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	core := w.ready(t, store.Task{Core: true})
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c == nil || c.TaskID != core {
		t.Fatalf("core claim = %+v", c)
	}
	review := w.ready(t, store.Task{Role: "review", Subject: core, Effect: "none"})
	if c := w.claim(t, "h2", store.LeaseTTL, cursor); c != nil {
		t.Fatalf("a review of core work went to %s", c.Tool.Executor)
	}
	if c := w.claim(t, "h2", store.LeaseTTL, codex); c == nil || c.TaskID != review {
		t.Fatalf("core review claim = %+v", c)
	}
}
