package store_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// salvagedTask is a task whose first attempt lost its lease and whose result
// its host then salvaged; with live, the task runs a new attempt meanwhile.
func (w *world) salvagedTask(t *testing.T, live bool) (task, old string, fresh *store.Claim) {
	t.Helper()
	task = w.ready(t, store.Task{})
	c := w.claim(t, "h1", -store.Grace-time.Second, codex)
	if c == nil || c.TaskID != task {
		t.Fatalf("claim = %+v", c)
	}
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	if w.state(t, "attempt", c.AttemptID) != "lost" || w.state(t, "task", task) != "ready" {
		t.Fatalf("after the sweep: attempt %s, task %s", w.state(t, "attempt", c.AttemptID), w.state(t, "task", task))
	}
	if live {
		fresh = w.claim(t, "h2", store.LeaseTTL, codex)
		if fresh == nil || fresh.TaskID != task {
			t.Fatalf("new attempt = %+v", fresh)
		}
		ok(t, store.Start(ctx, w.rt, fence(fresh, "h2"), "", ""))
	}
	calls := 0
	_, err = store.Salvage(ctx, w.rt, c.AttemptID, "h1", store.Result{Report: w.artifact(t, "report", "# salvaged "+task+"\n"),
		Ref: c.PushedRef, Commit: commitA}, heads(map[string]string{c.PushedRef: commitA}, &calls))
	ok(t, err)
	return task, c.AttemptID, fresh
}

// finish leases task itself to tool on host and carries it to submit (a
// claim would take the oldest ready task first).
func (w *world) finish(t *testing.T, task, host string, tool store.Tool, conclusion string) string {
	t.Helper()
	var attempt string
	var epoch int64
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		var err error
		attempt, epoch, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", task, tool.Executor, host, 1, store.LeaseTTL)
		if err != nil {
			return err
		}
		return store.MoveTaskForTest(ctx, tx, "controller", task, "leased")
	}))
	f := store.Fence{AttemptID: attempt, HostID: host, Epoch: epoch}
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	if conclusion != "done" {
		exit = 1
	}
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", conclusion)
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", host+conclusion+task)}, heads(nil, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	return attempt
}

// T34 case 8 (review 152 A3; Owner 10-14 option A): a lost attempt's
// salvaged result is verified and reviewed while the task runs a new attempt;
// the review's pass accepts the lost attempt, cancels the new one in the same
// transaction, and accepts the task, with one accepting verification.
func TestAdoptSalvagedResult(t *testing.T) {
	w := newKernelWorld(t)
	task, old, fresh := w.salvagedTask(t, true)
	pending, err := store.PendingSalvaged(ctx, w.rt)
	ok(t, err)
	if !slices.Equal(pending, []string{old}) {
		t.Fatalf("pending = %v", pending)
	}
	v, err := store.StartSalvageCheck(ctx, w.rt, old)
	ok(t, err)
	if v == "" || w.state(t, "task", task) != "running" {
		t.Fatalf("salvage check %q; task %s", v, w.state(t, "task", task))
	}
	if pin := w.checkSpec(t, v); pin["salvage"] != true || pin["attempt"] != old || pin["commit"] != commitA || pin["check"] != "verify" {
		t.Fatalf("the check pins %v", pin)
	}
	if again, aerr := store.StartSalvageCheck(ctx, w.rt, old); aerr != nil || again != "" {
		t.Fatalf("a second start made %q (%v)", again, aerr)
	}
	if pending, err = store.PendingSalvaged(ctx, w.rt); err != nil || len(pending) != 0 {
		t.Fatalf("still pending: %v %v", pending, err)
	}

	vc := w.run(t, "h3", deterministic, "done")
	if vc.TaskID != v || vc.BaseCommit != commitA {
		t.Fatalf("verification claim = %+v", vc)
	}
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	if did != "review" || w.state(t, "task", task) != "running" || w.state(t, "attempt", fresh.AttemptID) != "running" {
		t.Fatalf("conclude did %q; task %s", did, w.state(t, "task", task))
	}
	review := w.checker(t, task, "review")
	if w.checkSpec(t, review)["salvage"] != true {
		t.Fatal("the review is not marked salvage")
	}
	rc := w.run(t, "h4", cursor, "done")
	if rc.TaskID != review {
		t.Fatalf("review claim = %+v", rc)
	}
	did, err = store.Conclude(ctx, w.rt, review)
	ok(t, err)
	if did != "accepted" || w.state(t, "task", task) != "accepted" || w.state(t, "attempt", fresh.AttemptID) != "cancelled" ||
		w.state(t, "attempt", old) != "lost" {
		t.Fatalf("conclude did %q; task %s, new attempt %s, old %s", did, w.state(t, "task", task),
			w.state(t, "attempt", fresh.AttemptID), w.state(t, "attempt", old))
	}
	if w.events(t, "attempt_cancelled", fresh.AttemptID) != 1 {
		t.Fatal("no attempt_cancelled for the new attempt")
	}
	got := w.verifications(t, task)
	if len(got) != 2 || got[0].attempt != old || got[0].accepts || got[1].attempt != old || !got[1].accepts {
		t.Fatalf("verifications = %+v", got)
	}
	_, err = store.Heartbeat(ctx, w.rt, fence(fresh, "h2"))
	is(t, err, store.ErrFenced)
	_, err = store.Submit(ctx, w.rt, fence(fresh, "h2"))
	is(t, err, store.ErrFenced)
	w.chainHolds(t, "mission:M-1")
}

// With no attempt running the task is accepted from ready; a salvaged result
// that fails its check moves nothing and is not checked again.
func TestAdoptWithoutALiveAttemptAndFailing(t *testing.T) {
	w := newKernelWorld(t)
	task, old, _ := w.salvagedTask(t, false)
	v, err := store.StartSalvageCheck(ctx, w.rt, old)
	ok(t, err)
	w.finish(t, v, "h3", deterministic, "done")
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	if did != "review" {
		t.Fatalf("conclude did %q", did)
	}
	w.finish(t, w.checker(t, task, "review"), "h4", cursor, "done")
	did, err = store.Conclude(ctx, w.rt, w.checker2(t, task, "review"))
	ok(t, err)
	if did != "accepted" || w.state(t, "task", task) != "accepted" {
		t.Fatalf("conclude did %q; task %s", did, w.state(t, "task", task))
	}

	w2 := newKernelWorld(t)
	task2, old2, fresh2 := w2.salvagedTask(t, true)
	v2, err := store.StartSalvageCheck(ctx, w2.rt, old2)
	ok(t, err)
	w2.finish(t, v2, "h3", deterministic, "failed")
	did, err = store.Conclude(ctx, w2.rt, v2)
	ok(t, err)
	if did != "closed" || w2.state(t, "task", task2) != "running" || w2.state(t, "attempt", fresh2.AttemptID) != "running" {
		t.Fatalf("a failed salvage check did %q; task %s", did, w2.state(t, "task", task2))
	}
	if got := w2.verifications(t, task2); len(got) != 1 || got[0].verdict != "fail" || got[0].accepts {
		t.Fatalf("verifications = %+v", got)
	}
	if pending, perr := store.PendingSalvaged(ctx, w2.rt); perr != nil || len(pending) != 0 {
		t.Fatalf("checked again: %v %v", pending, perr)
	}
}

// checker2 finds the subject's check of role in any state.
func (w *world) checker2(t *testing.T, subject, role string) string {
	t.Helper()
	var id string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.task WHERE subject_task_id = $1 AND role = $2 ORDER BY created_at DESC LIMIT 1`,
		subject, role).Scan(&id))
	return id
}

// The new attempt's submit and the acceptance meet: whichever takes the new
// attempt's row first wins, and the task ends with one result either way.
func TestAdoptRacesTheNewAttempt(t *testing.T) {
	for _, first := range []string{"acceptance", "submit"} {
		t.Run(first, func(t *testing.T) {
			w := newKernelWorld(t)
			task, old, fresh := w.salvagedTask(t, true)
			v, err := store.StartSalvageCheck(ctx, w.rt, old)
			ok(t, err)
			w.run(t, "h3", deterministic, "done")
			_, err = store.Conclude(ctx, w.rt, v)
			ok(t, err)
			review := w.checker(t, task, "review")
			w.run(t, "h4", cursor, "done")
			f := fence(fresh, "h2")
			exit := 0
			_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "done")
			ok(t, err)
			calls := 0
			_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "# new\n"), Ref: fresh.PushedRef, Commit: commitA},
				heads(map[string]string{fresh.PushedRef: commitA}, &calls))
			ok(t, err)
			// The staged call runs on the pool; w.rt, outside any transaction,
			// polls pg_stat_activity (a transaction sees one snapshot of it).
			pool := w.db.Pool(t, w.db.Runtime)
			deadline := time.Now().Add(20 * time.Second)
			var did string
			var submitErr, concludeErr error
			if first == "acceptance" {
				done := make(chan struct{})
				unhook := store.SetHook(func(point, arg string) {
					if point != "adopting" || arg != task {
						return
					}
					go func() { defer close(done); _, submitErr = store.Submit(ctx, pool, f) }()
					w.waitFor(t, "WHERE attempt_id = $1 FOR UPDATE", deadline)
				})
				did, concludeErr = store.Conclude(ctx, pool, review)
				unhook()
				<-done
				ok(t, concludeErr)
				if did != "accepted" || !errors.Is(submitErr, store.ErrFenced) || w.state(t, "task", task) != "accepted" ||
					w.state(t, "attempt", fresh.AttemptID) != "cancelled" {
					t.Fatalf("did %q, submit %v; task %s, new attempt %s", did, submitErr, w.state(t, "task", task), w.state(t, "attempt", fresh.AttemptID))
				}
			} else {
				done := make(chan struct{})
				unhook := store.SetHook(func(point, arg string) {
					if point != "submitting" || arg != fresh.AttemptID {
						return
					}
					go func() { defer close(done); did, concludeErr = store.Conclude(ctx, pool, review) }()
					w.waitFor(t, "state IN ('leased','running','stale') FOR UPDATE", deadline)
				})
				_, submitErr = store.Submit(ctx, pool, f)
				unhook()
				<-done
				ok(t, submitErr)
				ok(t, concludeErr)
				if did != "closed" || w.state(t, "task", task) != "submitted" || w.state(t, "attempt", fresh.AttemptID) != "submitted" {
					t.Fatalf("did %q; task %s, new attempt %s", did, w.state(t, "task", task), w.state(t, "attempt", fresh.AttemptID))
				}
			}
			var accepting int
			ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.verification WHERE task_id = $1 AND accepts_attempt`, task).Scan(&accepting))
			if want := map[string]int{"acceptance": 1, "submit": 0}[first]; accepting != want {
				t.Fatalf("%d accepting verifications, want %d", accepting, want)
			}
			w.chainHolds(t, "mission:M-1")
		})
	}
}

// A new attempt that is claimed after the acceptance looked for a live
// attempt, and before it locked the task, is not left running under an
// accepted task: the acceptance waits for the next pass, which cancels it.
func TestAdoptLiveAttemptAppears(t *testing.T) {
	w := newKernelWorld(t)
	task, old, _ := w.salvagedTask(t, false)
	v, err := store.StartSalvageCheck(ctx, w.rt, old)
	ok(t, err)
	w.finish(t, v, "h3", deterministic, "done")
	_, err = store.Conclude(ctx, w.rt, v)
	ok(t, err)
	review := w.checker(t, task, "review")
	w.finish(t, review, "h4", cursor, "done")
	pool := w.db.Pool(t, w.db.Runtime)
	var claimed *store.Claim
	fired := false
	unhook := store.SetHook(func(point, arg string) {
		if point != "adopting" || arg != task || fired {
			return
		}
		fired = true
		var cerr error
		claimed, cerr = store.ClaimFor(ctx, pool, store.ClaimRequest{HostID: "h5", FreeSlots: 1, Tools: []store.Tool{codex}}, store.LeaseTTL)
		ok(t, cerr)
	})
	did, err := store.Conclude(ctx, w.rt, review)
	unhook()
	ok(t, err)
	if !fired || claimed == nil || claimed.TaskID != task || did != "" || w.state(t, "task", task) != "leased" {
		t.Fatalf("did %q with a new attempt %+v; task %s", did, claimed, w.state(t, "task", task))
	}
	did, err = store.Conclude(ctx, w.rt, review)
	ok(t, err)
	if did != "accepted" || w.state(t, "task", task) != "accepted" || w.state(t, "attempt", claimed.AttemptID) != "cancelled" {
		t.Fatalf("next pass did %q; task %s, new attempt %s", did, w.state(t, "task", task), w.state(t, "attempt", claimed.AttemptID))
	}
}

// A lost attempt salvaged after a later attempt was submitted (and failed
// its check, so the task is ready again in the next round) is not checked:
// the task went on without it.
func TestNoSalvageCheckAfterALaterSubmit(t *testing.T) {
	w := newKernelWorld(t)
	task := w.ready(t, store.Task{})
	old := w.claim(t, "h1", -store.Grace-time.Second, codex)
	_, _, err := store.SweepLeases(ctx, w.rt)
	ok(t, err)
	fresh := w.claim(t, "h2", store.LeaseTTL, codex)
	f := fence(fresh, "h2")
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "done")
	ok(t, err)
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "# new\n"), Ref: fresh.PushedRef, Commit: commitA},
		heads(map[string]string{fresh.PushedRef: commitA}, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	v, err := store.StartVerification(ctx, w.rt, task)
	ok(t, err)
	w.finish(t, v, "h3", deterministic, "failed")
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	if did != "rework" || w.state(t, "task", task) != "ready" {
		t.Fatalf("conclude did %q; task %s", did, w.state(t, "task", task))
	}
	_, err = store.Salvage(ctx, w.rt, old.AttemptID, "h1", store.Result{Report: w.artifact(t, "report", "# late\n"), Ref: old.PushedRef, Commit: commitA},
		heads(map[string]string{old.PushedRef: commitA}, &calls))
	ok(t, err)
	if pending, perr := store.PendingSalvaged(ctx, w.rt); perr != nil || len(pending) != 0 {
		t.Fatalf("pending = %v %v", pending, perr)
	}
	if id, serr := store.StartSalvageCheck(ctx, w.rt, old.AttemptID); serr != nil || id != "" {
		t.Fatalf("a check of the late salvage: %q %v", id, serr)
	}
}

// handSalvage proposes, by hand, a check of a salvaged result: a spec the
// controller did not write, marked salvage.
func (w *world) handSalvage(t *testing.T, check, subject, attempt string, round int, verification string) string {
	t.Helper()
	id := store.NewTaskID()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		doc := map[string]any{"check": check, "subject_task": subject, "attempt": attempt, "round": round, "task_spec": w.spec,
			"commit": commitA, "repo": "bifrost-platform", "salvage": true}
		if verification != "" {
			doc["verification"] = verification
		}
		b, err := canon.Encode(doc)
		if err != nil {
			return err
		}
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "application/json", b)
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: id, MissionID: "M-1", PlanNode: s1, Subject: subject,
			Role: check, Class: "go", Title: check, SpecArtifact: spec, Effect: "none", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", id)
	}))
	return id
}

// Review 166 A1: the salvage mark is only a claim. A check proposed by hand
// is judged on the state at acceptance as discovery judges it: after a later
// attempt was submitted (even pinned to the current round), after the
// result failed a check, or as a review pinned to a deterministic pass that
// is not the attempt's, it closes and moves nothing. (A review with no
// deterministic pass at all is closed by the check every review meets.)
func TestHandSalvagePins(t *testing.T) {
	cases := map[string]func(t *testing.T, w *world) (task, check string){
		"after a later submit": func(t *testing.T, w *world) (string, string) {
			task := w.ready(t, store.Task{})
			old := w.claim(t, "h1", -store.Grace-time.Second, codex)
			_, _, err := store.SweepLeases(ctx, w.rt)
			ok(t, err)
			fresh := w.claim(t, "h2", store.LeaseTTL, codex)
			f := fence(fresh, "h2")
			ok(t, store.Start(ctx, w.rt, f, "", ""))
			exit := 0
			_, err = store.Computed(ctx, w.rt, f, &exit, "exit", "done")
			ok(t, err)
			calls := 0
			_, err = store.Persist(ctx, w.rt, f, store.Result{Report: w.artifact(t, "report", "# new\n"), Ref: fresh.PushedRef, Commit: commitA},
				heads(map[string]string{fresh.PushedRef: commitA}, &calls))
			ok(t, err)
			_, err = store.Submit(ctx, w.rt, f)
			ok(t, err)
			v, err := store.StartVerification(ctx, w.rt, task)
			ok(t, err)
			w.finish(t, v, "h3", deterministic, "failed")
			_, err = store.Conclude(ctx, w.rt, v)
			ok(t, err)
			_, err = store.Salvage(ctx, w.rt, old.AttemptID, "h1", store.Result{Report: w.artifact(t, "report", "# late\n"), Ref: old.PushedRef, Commit: commitA},
				heads(map[string]string{old.PushedRef: commitA}, &calls))
			ok(t, err)
			return task, w.handSalvage(t, "verify", task, old.AttemptID, 2, "")
		},
		"after a failed check": func(t *testing.T, w *world) (string, string) {
			task, old, _ := w.salvagedTask(t, true)
			v, err := store.StartSalvageCheck(ctx, w.rt, old)
			ok(t, err)
			w.finish(t, v, "h3", deterministic, "failed")
			_, err = store.Conclude(ctx, w.rt, v)
			ok(t, err)
			return task, w.handSalvage(t, "verify", task, old, 1, "")
		},
		"a review whose pass is not its own": func(t *testing.T, w *world) (string, string) {
			task, old, _ := w.salvagedTask(t, true)
			v, err := store.StartSalvageCheck(ctx, w.rt, old)
			ok(t, err)
			w.finish(t, v, "h3", deterministic, "done")
			did, err := store.Conclude(ctx, w.rt, v)
			ok(t, err)
			if did != "review" {
				t.Fatalf("the deterministic check did %q", did)
			}
			return task, w.handSalvage(t, "review", task, old, 1, store.NewVerificationID())
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			w := newKernelWorld(t)
			task, check := setup(t, w)
			before := len(w.verifications(t, task))
			state := w.state(t, "task", task)
			w.finish(t, check, "h4", cursor, "done")
			did, err := store.Conclude(ctx, w.rt, check)
			ok(t, err)
			if did != "closed" || len(w.verifications(t, task)) != before || w.state(t, "task", task) != state {
				t.Fatalf("did %q; %d verifications (was %d); task %s (was %s)", did, len(w.verifications(t, task)), before, w.state(t, "task", task), state)
			}
		})
	}
}

// A salvaged result whose review fails moves nothing: the new attempt runs
// on, and nothing is accepted.
func TestSalvageReviewFails(t *testing.T) {
	w := newKernelWorld(t)
	task, old, fresh := w.salvagedTask(t, true)
	v, err := store.StartSalvageCheck(ctx, w.rt, old)
	ok(t, err)
	w.run(t, "h3", deterministic, "done")
	_, err = store.Conclude(ctx, w.rt, v)
	ok(t, err)
	review := w.checker(t, task, "review")
	w.run(t, "h4", cursor, "failed")
	did, err := store.Conclude(ctx, w.rt, review)
	ok(t, err)
	if did != "closed" || w.state(t, "task", task) != "running" || w.state(t, "attempt", fresh.AttemptID) != "running" {
		t.Fatalf("did %q; task %s, new attempt %s", did, w.state(t, "task", task), w.state(t, "attempt", fresh.AttemptID))
	}
	if got := w.verifications(t, task); len(got) != 2 || got[1].verdict != "fail" || got[1].accepts {
		t.Fatalf("verifications = %+v", got)
	}
}

// Two controller passes at once: one check of a salvaged result, one
// acceptance, one cancelled attempt.
func TestAdoptTwoPassesAtOnce(t *testing.T) {
	w := newKernelWorld(t)
	task, old, fresh := w.salvagedTask(t, true)
	pool := w.db.Pool(t, w.db.Runtime)
	both := func(f func() (string, error)) []string {
		var wg sync.WaitGroup
		out := make([]string, 2)
		errs := make([]error, 2)
		start := make(chan struct{})
		for i := range out {
			wg.Add(1)
			go func() { defer wg.Done(); <-start; out[i], errs[i] = f() }()
		}
		close(start)
		wg.Wait()
		ok(t, errors.Join(errs...))
		slices.Sort(out)
		return out
	}
	ids := both(func() (string, error) { return store.StartSalvageCheck(ctx, pool, old) })
	if ids[0] != "" || ids[1] == "" {
		t.Fatalf("two starts made %q", ids)
	}
	w.run(t, "h3", deterministic, "done")
	_, err := store.Conclude(ctx, w.rt, ids[1])
	ok(t, err)
	review := w.checker(t, task, "review")
	w.run(t, "h4", cursor, "done")
	dids := both(func() (string, error) { return store.Conclude(ctx, pool, review) })
	if dids[0] != "" || dids[1] != "accepted" || w.events(t, "attempt_cancelled", fresh.AttemptID) != 1 {
		t.Fatalf("two conclusions did %q; %d cancellations", dids, w.events(t, "attempt_cancelled", fresh.AttemptID))
	}
	var accepting int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.verification WHERE task_id = $1 AND accepts_attempt`, task).Scan(&accepting))
	if accepting != 1 {
		t.Fatalf("%d accepting verifications", accepting)
	}
}
