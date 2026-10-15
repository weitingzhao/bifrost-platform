package store_test

import (
	"errors"
	"sync"
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

// A2: a pause, a stop or a drain that commits while a claim is between
// reading its candidates and locking them is seen by that claim.
func TestClaimSeesAControlThatLandsMidway(t *testing.T) {
	for _, tc := range []struct {
		name, waits string
		set         func(tx pgx.Tx) error
	}{
		{"mission paused", "COALESCE(envelope_rule_id, '') FROM agentrt.mission",
			func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-1", "paused", "test") }},
		{"mission stop", "COALESCE(envelope_rule_id, '') FROM agentrt.mission",
			func(tx pgx.Tx) error {
				return store.SetControl(ctx, tx, "owner", "c1", "mission", "M-1", "stop", "test")
			}},
		{"global pause", "pg_advisory_xact_lock_shared",
			func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", "c1", "global", "", "pause", "test") }},
		{"host drain", "UPDATE agentrt.host SET last_poll_at",
			func(tx pgx.Tx) error { return store.SetControl(ctx, tx, "owner", "c1", "host", "h1", "drain", "test") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newKernelWorld(t)
			task := w.ready(t, store.Task{})
			pool := w.db.Pool(t, w.db.Runtime)
			tx, err := pool.Begin(ctx)
			ok(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			ok(t, tc.set(tx))
			done := make(chan claimed, 1)
			go func() {
				c, err := store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{claude}})
				done <- claimed{c, err}
			}()
			w.waitFor(t, tc.waits, time.Now().Add(10*time.Second))
			ok(t, tx.Commit(ctx))
			got := <-done
			ok(t, got.err)
			if got.c != nil || w.state(t, "task", task) != "ready" {
				t.Fatalf("claimed %+v after the control committed", got.c)
			}
		})
	}

	// A control on the task holds the task row: the claim skips it rather
	// than wait, and claims after the control commits see it.
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	pool := w.db.Pool(t, w.db.Runtime)
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	ok(t, store.SetControl(ctx, tx, "owner", "c1", "task", task, "pause", "test"))
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claimed %s while its control was being set", c.TaskID)
	}
	ok(t, tx.Commit(ctx))
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claimed %s under a task pause", c.TaskID)
	}
}

// A3: Start and Submit move the task, so they lock it before reading the
// clock; a wait for the task row that crosses the deadline is refused and
// leaves no event, no state change and no idempotency key.
func TestTaskLockWaitCrossesTheDeadline(t *testing.T) {
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

	w.ready(t, store.Task{})
	c := w.claim(t, "h1", -store.Grace+2*time.Second, codex)
	deadline := c.LeaseExpiresAt.Add(store.Grace)
	holder := hold(c.TaskID)
	done := make(chan error, 1)
	go func() { done <- store.Start(ctx, pool, fence(c, "h1"), "", "") }()
	w.waitFor(t, taskLock, deadline)
	time.Sleep(time.Until(deadline) + time.Second)
	ok(t, holder.Commit(ctx))
	is(t, <-done, store.ErrFenced)
	if w.state(t, "attempt", c.AttemptID) != "leased" || w.events(t, "attempt_started", c.AttemptID) != 0 {
		t.Fatal("a refused start changed the attempt")
	}

	w.ready(t, store.Task{Effect: "none"})
	s := w.claim(t, "h2", -store.Grace+4*time.Second, codex)
	f := fence(s, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "# none\n")}, nil)
	ok(t, err)
	deadline = s.LeaseExpiresAt.Add(store.Grace)
	holder = hold(s.TaskID)
	go func() {
		_, err := store.Submit(ctx, pool, f)
		done <- err
	}()
	w.waitFor(t, taskLock, deadline)
	time.Sleep(time.Until(deadline) + time.Second)
	ok(t, holder.Commit(ctx))
	is(t, <-done, store.ErrFenced)
	var keys int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
		"host:h2|submit|"+s.AttemptID).Scan(&keys))
	if w.events(t, "attempt_submitted", s.AttemptID) != 0 || w.state(t, "task", s.TaskID) != "running" || keys != 0 {
		t.Fatalf("a refused submit left %d keys or changed state", keys)
	}
}

// A4: external work is not dispatched in round 2.
func TestExternalWorkIsNotDispatched(t *testing.T) {
	w := newKernelWorld(t)
	w.ready(t, store.Task{Effect: "external"})
	c, err := store.ClaimNext(ctx, w.rt, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{codex, claude, cursor}})
	ok(t, err)
	if c != nil {
		t.Fatalf("external task %s was dispatched", c.TaskID)
	}
}

// B1: a candidate the claim looks at and does not take is unlocked before
// the claim goes on.
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
	w := newKernelWorld(t)
	for i := 0; i < 100; i++ {
		w.ready(t, store.Task{Needs: []string{"git_commit"}})
	}
	last := w.ready(t, store.Task{})
	if c := w.claim(t, "h1", store.LeaseTTL, codex); c == nil || c.TaskID != last {
		t.Fatalf("claim = %+v; want %s behind 100 tasks codex may not take", c, last)
	}
}

// B3: stale attempts within their grace do not crowd a newly lapsed one out
// of the sweep.
func TestSweepIsNotCrowdedByStaleAttempts(t *testing.T) {
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for i := 0; i < 100; i++ {
			id := store.NewTaskID()
			if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: s1, Role: "implement",
				Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "branch"}); err != nil {
				return err
			}
			if _, _, err := store.LeaseAttempt(ctx, tx, "controller", "M-1", id, "exec:codex", "", 1, -time.Minute); err != nil {
				return err
			}
		}
		return nil
	}))
	stale, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if stale != 100 {
		t.Fatalf("first sweep: %d stale", stale)
	}
	w.ready(t, store.Task{})
	late := w.claim(t, "h1", -time.Second, codex)
	stale, lost, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if stale != 1 || lost != 0 || w.state(t, "attempt", late.AttemptID) != "stale" {
		t.Fatalf("second sweep: %d stale, %d lost", stale, lost)
	}
}

// B4: a review of core work is core work even when its proposer left Core
// out.
func TestReviewOfCoreWorkIsCore(t *testing.T) {
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

// B5: branches the first tests did not reach.
func TestKernelBranches(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)

	// Two sweeps at once move each lapsed attempt once.
	var lapsed []string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for i := 0; i < 10; i++ {
			id := store.NewTaskID()
			if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: s1, Role: "implement",
				Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "branch"}); err != nil {
				return err
			}
			a, _, err := store.LeaseAttempt(ctx, tx, "controller", "M-1", id, "exec:codex", "", 1, -time.Minute)
			if err != nil {
				return err
			}
			lapsed = append(lapsed, a)
		}
		return nil
	}))
	var counts [2]int
	var errs [2]error
	var wg sync.WaitGroup
	for i := range counts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			counts[i], _, errs[i] = store.SweepLeases(ctx, pool)
		}(i)
	}
	wg.Wait()
	ok(t, errs[0])
	ok(t, errs[1])
	if counts[0]+counts[1] != 10 {
		t.Fatalf("parallel sweeps moved %d + %d", counts[0], counts[1])
	}
	for _, a := range lapsed {
		if w.events(t, "attempt_stale", a) != 1 {
			t.Fatalf("%s has %d stale events", a, w.events(t, "attempt_stale", a))
		}
	}

	// running → stale → renewed comes back running.
	w.ready(t, store.Task{})
	r := w.claim(t, "h1", -time.Second, codex)
	ok(t, store.Start(ctx, w.rt, fence(r, "h1"), "", ""))
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if w.state(t, "attempt", r.AttemptID) != "stale" {
		t.Fatal("not stale")
	}
	_, err = store.Heartbeat(ctx, w.rt, fence(r, "h1"))
	ok(t, err)
	if w.state(t, "attempt", r.AttemptID) != "running" {
		t.Fatal("a renewed running attempt did not come back running")
	}

	// A task of effect none: no branch; a failed submit stores no key; a
	// different epoch after the submit is a conflict.
	w.ready(t, store.Task{Effect: "none"})
	n := w.claim(t, "h2", store.LeaseTTL, codex)
	f := fence(n, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	_, err = store.Submit(ctx, w.rt, f)
	is(t, err, store.ErrState)
	var keys int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
		"host:h2|submit|"+n.AttemptID).Scan(&keys))
	if keys != 0 {
		t.Fatal("a failed submit stored a key")
	}
	exit := 1
	_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "failed")
	ok(t, err)
	report := w.artifact(t, "report", "# failed\n")
	calls := 0
	head := heads(map[string]string{n.PushedRef: commitA}, &calls)
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: report, Ref: n.PushedRef, Commit: commitA}, head)
	is(t, err, store.ErrState)
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: report}, head)
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, store.Fence{AttemptID: n.AttemptID, HostID: "h2", Epoch: n.Epoch + 1})
	is(t, err, store.ErrConflict)

	// Facts scoped to a provider bind only that provider; retired ones bind
	// nothing.
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: provider-scoped facts"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.capability_fact (capability_fact_id, executor_actor_id, model_provider, fact_kind, subject, source_kind, source)
		  VALUES ('claude-anthropic-no-gpu', 'exec:claude', 'anthropic', 'cannot', 'gpu', 'measured', 'test'),
		         ('codex-openai-only-vault', 'exec:codex', 'openai', 'only', 'vault', 'owner', 'test')`)
		return err
	}))
	otherClaude := store.Tool{Executor: "exec:claude", Provider: "other"}
	otherCodex := store.Tool{Executor: "exec:codex", Provider: "other"}
	gpu := w.ready(t, store.Task{Needs: []string{"gpu"}})
	if c := w.claim(t, "h3", store.LeaseTTL, claude); c != nil {
		t.Fatalf("claude/anthropic took %s, which needs gpu", c.TaskID)
	}
	if c := w.claim(t, "h3", store.LeaseTTL, otherClaude); c == nil || c.TaskID != gpu {
		t.Fatalf("claude/other claim = %+v", c)
	}
	vault := w.ready(t, store.Task{Needs: []string{"vault"}})
	if c := w.claim(t, "h4", store.LeaseTTL, otherCodex, claude); c != nil {
		t.Fatalf("%+v took %s, which only codex/openai may", c.Tool, c.TaskID)
	}
	if c := w.claim(t, "h4", store.LeaseTTL, codex); c == nil || c.TaskID != vault {
		t.Fatalf("codex/openai claim = %+v", c)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: retire"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.capability_fact SET retired_at = clock_timestamp() WHERE capability_fact_id = 'claude-anthropic-no-gpu'`)
		return err
	}))
	gpu2 := w.ready(t, store.Task{Needs: []string{"gpu"}})
	if c := w.claim(t, "h5", store.LeaseTTL, claude); c == nil || c.TaskID != gpu2 {
		t.Fatalf("after retiring the fact, claude/anthropic claim = %+v", c)
	}

	// Steps move along their table only.
	for _, move := range [][2]string{{s2, "dropped"}, {s2, "active"}, {s3, "planned"}} {
		err := store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			_, err := store.SetStepState(ctx, tx, "controller", root, move[0], move[1], "test")
			return err
		})
		if move[1] == "dropped" {
			ok(t, err)
			continue
		}
		is(t, err, store.ErrState)
	}
	w.chainHolds(t, "mission:M-1")
}
