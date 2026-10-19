package store_test

import (
	"testing"
	"time"

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

// A2: independence holds at the moment of the lease. An author attempt of
// the subject that lands after the reviewer picked its tool makes the review
// refuse; while the reviewer holds the subject, no author attempt lands.
//
// Serial: it holds the package's test hook (SetHook).
func TestReviewIndependenceUnderTheSubjectLock(t *testing.T) {
	// codex cannot commit (seed), so it passes over the subject, which needs
	// to, and reviews it; cursor writes it. This reviewer shares cursor's
	// provider.
	reviewer := store.Tool{Executor: "exec:codex", Provider: "cursor-mix"}

	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	subject := w.ready(t, store.Task{Needs: []string{"git_commit"}})
	review := w.ready(t, store.Task{Role: "review", Subject: subject, Effect: "none"})
	var author *store.Claim
	unhook := store.SetHook(func(point, arg string) {
		if point != "picked" || arg != review || author != nil {
			return
		}
		var err error
		author, err = store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h2", FreeSlots: 1, Tools: []store.Tool{cursor}})
		ok(t, err)
	})
	c := w.claim(t, "h1", store.LeaseTTL, reviewer)
	unhook()
	if author == nil || author.TaskID != subject {
		t.Fatalf("author claim = %+v", author)
	}
	if c != nil || w.state(t, "task", review) != "ready" {
		t.Fatalf("the review went to %+v after the same provider wrote its subject", c)
	}

	w = newKernelWorld(t)
	pool = w.db.Pool(t, w.db.Runtime)
	subject = w.ready(t, store.Task{Needs: []string{"git_commit"}})
	review = w.ready(t, store.Task{Role: "review", Subject: subject, Effect: "none"})
	tried := false
	unhook = store.SetHook(func(point, arg string) {
		if point != "taken" || arg != review || tried {
			return
		}
		tried = true
		a, err := store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h2", FreeSlots: 1, Tools: []store.Tool{cursor}})
		ok(t, err)
		if a != nil {
			t.Errorf("an author attempt of %s landed while its review was being leased", a.TaskID)
		}
	})
	c = w.claim(t, "h1", store.LeaseTTL, reviewer)
	unhook()
	if !tried || c == nil || c.TaskID != review {
		t.Fatalf("review claim = %+v", c)
	}
}

// B1: writes take their row locks before their stream. A review proposed
// while a claim holds its subject waits for the claim instead of closing a
// cycle with it.
//
// Serial: it holds the package's test hook (SetHook).
func TestProposeDuringAClaimDoesNotDeadlock(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	task := w.ready(t, store.Task{})
	proposed := make(chan error, 1)
	started := false
	unhook := store.SetHook(func(point, arg string) {
		if point != "taken" || arg != task || started {
			return
		}
		started = true
		go func() {
			proposed <- store.InTx(ctx, pool, func(tx pgx.Tx) error {
				_, err := store.ProposeTask(ctx, tx, "controller", store.Task{MissionID: "M-1", PlanNode: s1, Subject: task, Role: "review",
					Class: "go", Title: "review", SpecArtifact: w.spec, Effect: "none"})
				return err
			})
		}()
		w.waitFor(t, "agentrt.task", time.Now().Add(10*time.Second))
	})
	// The claim runs on the pool so that w.rt, outside any transaction, sees
	// the proposal wait (a transaction keeps its first view of pg_stat_activity).
	c, err := store.ClaimNext(ctx, pool, store.ClaimRequest{HostID: "h1", FreeSlots: 1, Tools: []store.Tool{claude}})
	unhook()
	ok(t, err)
	if c == nil || c.TaskID != task {
		t.Fatalf("claim = %+v", c)
	}
	ok(t, <-proposed)
}

// B2: a sweep holds one attempt, and so one stream, at a time. While one
// sweep is inside an attempt of M-1, another moves an attempt of M-2.
//
// Serial: it holds the package's test hook (SetHook).
func TestSweepHoldsOneStreamAtATime(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	var first, second string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		for _, x := range []struct {
			mission string
			ago     time.Duration
			id      *string
		}{{"M-1", 2 * time.Minute, &first}, {"M-2", time.Minute, &second}} {
			task := store.Task{ID: store.NewTaskID(), MissionID: x.mission, Role: "implement", Class: "go", Title: "t",
				SpecArtifact: w.spec, Effect: "branch"}
			if x.mission == "M-1" {
				task.PlanNode = s1
			} else {
				task.OffPlanReason = "outside the plan"
			}
			if _, err := store.ProposeTask(ctx, tx, "controller", task); err != nil {
				return err
			}
			var err error
			if *x.id, _, err = store.LeaseAttempt(ctx, tx, "controller", x.mission, task.ID, "exec:codex", "", 1, -x.ago); err != nil {
				return err
			}
		}
		return nil
	}))
	type swept struct {
		stale int
		err   error
	}
	other := make(chan swept, 1)
	ran := false
	var released error
	probed := false
	unhook := store.SetHook(func(point, arg string) {
		if point == "committed" && arg == first && !probed {
			// Review 150 B3: once its attempt is done, a sweep holds no stream.
			probed = true
			tx, err := pool.Begin(ctx)
			if err != nil {
				released = err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, released = tx.Exec(ctx, `SELECT 1 FROM agentrt.event_stream WHERE event_stream_id = 'mission:M-1' FOR UPDATE NOWAIT`)
			return
		}
		if point != "swept" || arg != first || ran {
			return
		}
		ran = true
		go func() {
			stale, _, err := store.SweepLeases(ctx, pool)
			other <- swept{stale, err}
		}()
		select {
		case s := <-other:
			other <- s
		case <-time.After(10 * time.Second):
			t.Errorf("the second sweep waited for the first")
		}
	})
	stale, _, err := store.SweepLeases(ctx, w.rt)
	unhook()
	ok(t, err)
	if !ran || !probed {
		t.Fatalf("the first sweep reached its first attempt %v, committed it %v", ran, probed)
	}
	ok(t, released)
	s := <-other
	ok(t, s.err)
	if !ran || stale != 1 || s.stale != 1 || w.state(t, "attempt", first) != "stale" || w.state(t, "attempt", second) != "stale" {
		t.Fatalf("first sweep moved %d, the second %d", stale, s.stale)
	}
}

// Coverage review 149 B3 listed as still missing.
func TestKernelBranches149(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)

	// Core review work with no holder of only review_core goes to nobody.
	core := w.ready(t, store.Task{Core: true})
	if c := w.claim(t, "h1", store.LeaseTTL, claude); c == nil || c.TaskID != core {
		t.Fatalf("core claim = %+v", c)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: retire the core reviewer fact"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.capability_fact SET retired_at = clock_timestamp() WHERE capability_fact_id = 'core-review-codex-readonly'`)
		return err
	}))
	w.ready(t, store.Task{Role: "review", Subject: core, Effect: "none"})
	if c := w.claim(t, "h2", store.LeaseTTL, codex, cursor, store.Tool{Executor: "exec:claude", Provider: "other"}); c != nil {
		t.Fatalf("core review with no reviewer left went to %+v", c.Tool)
	}

	// A refused submit changes nothing: no event, no state, no key.
	w.ready(t, store.Task{Effect: "none"})
	n := w.claim(t, "h3", store.LeaseTTL, codex)
	f := fence(n, "h3")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	_, err := store.Submit(ctx, w.rt, f)
	is(t, err, store.ErrState)
	var keys int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.idempotency_key WHERE idempotency_key_id = $1`,
		"host:h3|submit|"+n.AttemptID).Scan(&keys))
	if keys != 0 || w.events(t, "attempt_submitted", n.AttemptID) != 0 || w.state(t, "attempt", n.AttemptID) != "running" ||
		w.state(t, "task", n.TaskID) != "running" {
		t.Fatal("a refused submit left a trace")
	}

	// The sweep passes over an attempt a writer holds and takes it next time.
	w.ready(t, store.Task{})
	held := w.claim(t, "h4", -time.Second, codex)
	pool := w.db.Pool(t, w.db.Runtime)
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.attempt WHERE attempt_id = $1 FOR UPDATE`, held.AttemptID)
	ok(t, err)
	stale, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if stale != 0 || w.state(t, "attempt", held.AttemptID) != "leased" {
		t.Fatal("the sweep moved an attempt a writer held")
	}
	ok(t, tx.Commit(ctx))
	stale, _, err = store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if stale != 1 {
		t.Fatalf("the sweep did not take the attempt once it was free: %d", stale)
	}
	w.chainHolds(t, "mission:M-1")
}
