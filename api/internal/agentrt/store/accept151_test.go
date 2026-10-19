package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Regression tests for review 151.

// gatePass records a passing gate verification of test on env for a task
// and attempt of step, at commit and ref.
func (w *world) gateVerification(t *testing.T, task, attempt, method, verdict, test, env, commit, spec string, ref map[string]any) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		ev, err := store.PutArtifact(ctx, tx, "controller", "verify", "text/plain", []byte(method+verdict+test+env+commit+spec))
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO agentrt.verification (verification_id, task_id, attempt_id, method, check_ref, env, verdict,
		  evidence_artifact_id, subject_commit, deploy_ref_json, spec_sha256) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			store.NewVerificationID(), task, attempt, method, test, env, verdict, ev, commit, ref, spec)
		return err
	}))
}

func (w *world) taskWithAttempt(t *testing.T, step string) (string, string) {
	t.Helper()
	task := store.NewTaskID()
	var attempt string
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-1", PlanNode: step, Role: "implement",
			Class: "go", Title: "t", SpecArtifact: w.spec, Effect: "none"}); err != nil {
			return err
		}
		var err error
		attempt, _, err = store.LeaseAttempt(ctx, tx, "controller", "M-1", task, "exec:deterministic", "", 1, time.Minute)
		return err
	}))
	return task, attempt
}

// A1: every gate of an environment is checked against one deployment, even
// when the environment changes between two gates of the rule.
func TestGatesShareOneDeployment(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	spec := strings.Repeat("ab", 32)
	step := "R2:2.1-4"
	w.step(t, step, 3, `{"gates_passed":[{"test":"T19","spec_sha256":"`+spec+`"},{"test":"T20","spec_sha256":"`+spec+`"}],"env":"stg"}`)
	refA, refB := map[string]any{"run": "a"}, map[string]any{"run": "b"}
	task, attempt := w.taskWithAttempt(t, step)
	w.gateVerification(t, task, attempt, "gate", "pass", "T19", "stg", commitA, spec, refA)
	w.gateVerification(t, task, attempt, "gate", "pass", "T20", "stg", commitB, spec, refB)
	reads := 0
	flipping := func(context.Context, string) (string, map[string]any, bool) {
		reads++
		if reads == 1 {
			return commitA, refA, true
		}
		return commitB, refB, true
	}
	moved, err := store.EvaluateStep(ctx, w.rt, step, flipping)
	ok(t, err)
	if moved != "" || reads != 1 {
		t.Fatalf("moved %q after %d reads of the deployment", moved, reads)
	}
}

// B1: a gate counts only as a passing verification of method gate.
func TestGateCountsOnlyPassingGates(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	spec := strings.Repeat("ab", 32)
	step := "R2:2.1-4"
	w.step(t, step, 3, `{"gates_passed":[{"test":"T19","spec_sha256":"`+spec+`"}],"env":"stg"}`)
	ref := map[string]any{"run": "a"}
	d := &deployments{}
	d.set("stg", commitA, ref)
	task, attempt := w.taskWithAttempt(t, step)
	w.gateVerification(t, task, attempt, "review", "pass", "T19", "stg", commitA, spec, ref)
	w.gateVerification(t, task, attempt, "gate", "fail", "T19", "stg", commitA, spec, ref)
	if w.evaluate(t, step, d) != "" {
		t.Fatal("a review or a failing gate counted as a gate pass")
	}
	w.gateVerification(t, task, attempt, "gate", "pass", "T19", "stg", commitA, spec, ref)
	if w.evaluate(t, step, d) != "done" {
		t.Fatal("the passing gate did not count")
	}
}

// migration starts a plan change that holds two steps, as the guard does
// before it moves tasks.
func (w *world) migration(t *testing.T, pool interface {
	Begin(context.Context) (pgx.Tx, error)
}, steps ...string) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	ok(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	_, err = store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
		Body: map[string]any{"change": "move tasks"}})
	ok(t, err)
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = ANY($1) ORDER BY plan_node_id FOR UPDATE`, steps)
	ok(t, err)
	return tx
}

// A2: starting a verification and using a result take the step first, so a
// plan change that holds the step and then moves the tasks never closes a
// cycle with them; they see the move and try again on the next pass.
func TestAcceptanceWaitsForPlanChanges(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	m := w.migration(t, pool, s1, s3)
	type started struct {
		id  string
		err error
	}
	done := make(chan started, 1)
	go func() {
		id, err := store.StartVerification(ctx, pool, impl)
		done <- started{id, err}
	}()
	w.waitFor(t, "FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE", time.Now().Add(10*time.Second))
	_, err := m.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2 WHERE task_id = $1`, impl, s3)
	ok(t, err)
	ok(t, m.Commit(ctx))
	got := <-done
	ok(t, got.err)
	if got.id != "" || w.state(t, "task", impl) != "submitted" {
		t.Fatalf("started %q across a move", got.id)
	}
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if v == "" {
		t.Fatal("the next pass did not start the verification")
	}

	w.run(t, "h2", deterministic, "done")
	m = w.migration(t, pool, s3, s1)
	concluded := make(chan started, 1)
	go func() {
		did, concludeErr := store.Conclude(ctx, pool, v)
		concluded <- started{did, concludeErr}
	}()
	w.waitFor(t, "FROM agentrt.plan_node WHERE plan_node_id = $1 FOR SHARE", time.Now().Add(10*time.Second))
	for _, id := range []string{impl, v} {
		_, err = m.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = $2 WHERE task_id = $1`, id, s1)
		ok(t, err)
	}
	ok(t, m.Commit(ctx))
	got = <-concluded
	ok(t, got.err)
	if got.id != "" || w.state(t, "task", v) != "submitted" {
		t.Fatalf("concluded %q across a move", got.id)
	}
	if did, err := store.Conclude(ctx, w.rt, v); err != nil || did != "review" {
		t.Fatalf("the next pass concluded %q (%v)", did, err)
	}
}

// handCheck proposes a verification or review of subject the way the
// controller pins one, without the controller: a duplicate or an early one.
// pins are the spec's commit and repo; base is the check's own checkout.
func (w *world) handCheck(t *testing.T, check, subject, attempt string, round int, pins ...string) string {
	t.Helper()
	id := store.NewTaskID()
	commit, repo, base := "", "", ""
	if len(pins) == 3 {
		commit, repo, base = pins[0], pins[1], pins[2]
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		doc := map[string]any{"check": check, "subject_task": subject, "attempt": attempt, "round": round, "task_spec": w.spec}
		if commit != "" {
			doc["commit"] = commit
		}
		if repo != "" {
			doc["repo"] = repo
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
			Role: check, Class: "go", Title: check, SpecArtifact: spec, Effect: "none", Repo: repo, BaseCommit: base}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", id)
	}))
	return id
}

// A3: a check's result is used only for the attempt and round it pins; a
// review only after a deterministic pass of that attempt. Anything else
// closes the check and moves nothing.
func TestChecksAreAboutOneAttempt(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	first := w.run(t, "h1", cursor, "done")
	v1, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	duplicate := w.handCheck(t, "verify", impl, first.AttemptID, 1)
	if c := w.run(t, "h2", deterministic, "failed"); c.TaskID != v1 {
		t.Fatalf("took %s first", c.TaskID)
	}
	did, err := store.Conclude(ctx, w.rt, v1)
	if err != nil || did != "rework" {
		t.Fatalf("round 1: %q (%v)", did, err)
	}
	w.run(t, "h3", cursor, "done")
	_, err = store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if c := w.run(t, "h4", deterministic, "done"); c.TaskID != duplicate {
		t.Fatalf("took %s, not the round 1 duplicate", c.TaskID)
	}
	did, err = store.Conclude(ctx, w.rt, duplicate)
	ok(t, err)
	var reviews int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE subject_task_id = $1 AND role = 'review'`, impl).Scan(&reviews))
	if did != "closed" || w.state(t, "task", impl) != "verifying" || reviews != 0 || len(w.verifications(t, impl)) != 1 {
		t.Fatalf("a round 1 result moved round 2: %q, %d reviews", did, reviews)
	}

	w2 := newKernelWorld(t)
	impl2 := w2.ready(t, store.Task{})
	a := w2.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w2.rt, impl2)
	ok(t, err)
	if c := w2.claim(t, "h2", store.LeaseTTL, deterministic); c == nil || c.TaskID != v {
		t.Fatalf("verification claim = %+v", c)
	}
	early := w2.handCheck(t, "review", impl2, a.AttemptID, 1)
	if c := w2.run(t, "h3", codex, "done"); c.TaskID != early {
		t.Fatalf("took %s", c.TaskID)
	}
	did, err = store.Conclude(ctx, w2.rt, early)
	if err != nil || did != "closed" || w2.state(t, "task", impl2) != "verifying" {
		t.Fatalf("an early review: %q (%v), implementation %s", did, err, w2.state(t, "task", impl2))
	}
}

// B1: two controllers starting one verification start it once.
//
// Serial: it holds the package's test hook (SetHook).
func TestStartVerificationOnce(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	var arrived sync.WaitGroup
	arrived.Add(2)
	unhook := store.SetHook(func(point, _ string) {
		if point == "verifying" {
			arrived.Done()
			arrived.Wait()
		}
	})
	ids := make([]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = store.StartVerification(ctx, pool, impl)
		}(i)
	}
	wg.Wait()
	unhook()
	ok(t, errs[0])
	ok(t, errs[1])
	if (ids[0] == "") == (ids[1] == "") {
		t.Fatalf("started %q", ids)
	}
}

// A5: a source is read under its record's lock. A read that a newer one
// overtakes is not recorded as a change back.
func TestOutsideReadsUnderTheLock(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	_, err := store.SeeApproval(ctx, w.rt, 16, approvalIs("pending"))
	ok(t, err)
	inside, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	slow := make(chan error, 1)
	go func() {
		_, seeErr := store.SeeApproval(ctx, pool, 16, func(context.Context) (string, bool) {
			close(inside)
			<-release
			return "pending", true
		})
		slow <- seeErr
	}()
	<-inside
	fast := make(chan error, 1)
	go func() {
		_, seeErr := store.SeeApproval(ctx, pool, 16, approvalIs(store.ApprovalExecuted))
		fast <- seeErr
	}()
	w.waitFor(t, "pg_advisory_xact_lock(742104", time.Now().Add(10*time.Second))
	released.Do(func() { close(release) })
	ok(t, <-slow)
	ok(t, <-fast)
	var statuses []string
	rows, err := w.rt.Query(ctx, `SELECT body_json->>'status' FROM agentrt.event WHERE event_type = 'approval_seen' ORDER BY event_id`)
	ok(t, err)
	statuses, err = pgx.CollectRows(rows, pgx.RowTo[string])
	ok(t, err)
	if strings.Join(statuses, ",") != "pending,executed" {
		t.Fatalf("recorded %v", statuses)
	}

	// Changes seen in order are all kept: pending, executed, pending.
	_, err = store.SeeApproval(ctx, w.rt, 16, approvalIs("pending"))
	ok(t, err)
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'approval_seen'`).Scan(&n))
	if n != 3 {
		t.Fatalf("%d changes recorded; want pending, executed, pending", n)
	}
}

// B2: an answered question is copied whole, or not at all.
func TestAnswerIsCopiedWhole(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	_, err := store.SeeQuestion(ctx, w.rt, "Q3", questionIs(store.Question{Status: store.QuestionAnswered, Option: "A", AnsweredAt: "2026-10-14T12:00:00Z"}))
	is(t, err, store.ErrState)
	_, err = store.SeeQuestion(ctx, w.rt, "Q3", questionIs(answered("Q3", "A")))
	ok(t, err)
	var copy map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json FROM agentrt.event WHERE event_type = 'question_seen' ORDER BY event_id DESC LIMIT 1`).Scan(&copy))
	want := map[string]any{"question": "Q3", "status": "answered", "option": "A", "text": "yes", "answered_by": "owner",
		"answered_at": "2026-10-14T12:00:00Z"}
	for k, v := range want {
		if copy[k] != v {
			t.Fatalf("copy = %v", copy)
		}
	}
}

// Review 152 A1: a check counts only when it pins the attempt under
// verification and ran on that attempt's version: its commit, the subject's
// repo and task spec, and a checkout of that repo at that commit. A check
// bound right counts whoever proposed it.
func TestCheckVersionIsTheAttempts(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{Repo: "bifrost-platform"})
	first := w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if c := w.claim(t, "h2", store.LeaseTTL, deterministic); c == nil || c.TaskID != v {
		t.Fatalf("claim = %+v", c)
	}
	wrongBase := w.handCheck(t, "verify", impl, first.AttemptID, 1, commitA, "bifrost-platform", commitB)
	right := w.handCheck(t, "verify", impl, first.AttemptID, 1, commitA, "bifrost-platform", commitA)
	if c := w.run(t, "h3", deterministic, "done"); c.TaskID != wrongBase {
		t.Fatalf("took %s", c.TaskID)
	}
	did, concludeErr := store.Conclude(ctx, w.rt, wrongBase)
	if concludeErr != nil || did != "closed" {
		t.Fatalf("a check on another checkout: %q (%v)", did, concludeErr)
	}
	if c := w.run(t, "h4", deterministic, "done"); c.TaskID != right {
		t.Fatalf("took %s", c.TaskID)
	}
	did, concludeErr = store.Conclude(ctx, w.rt, right)
	if concludeErr != nil || did != "review" {
		t.Fatalf("a check bound right: %q (%v)", did, concludeErr)
	}
}

// Review 152 A1: an attempt of an earlier round, still submitted, cannot
// stand in for the current one by naming the current round.
func TestOldAttemptCannotPoseAsCurrent(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	first := w.run(t, "h1", cursor, "done")
	v1, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "failed")
	did, concludeErr := store.Conclude(ctx, w.rt, v1)
	if concludeErr != nil || did != "rework" {
		t.Fatalf("round 1: %q (%v)", did, concludeErr)
	}
	w.run(t, "h3", cursor, "done")
	_, err = store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	posing := w.handCheck(t, "verify", impl, first.AttemptID, 2, commitA, "", commitA)
	w.claim(t, "h4", store.LeaseTTL, deterministic) // the controller's own round 2 check
	if c := w.run(t, "h5", deterministic, "done"); c.TaskID != posing {
		t.Fatalf("took %s", c.TaskID)
	}
	did, concludeErr = store.Conclude(ctx, w.rt, posing)
	if concludeErr != nil || did != "closed" || len(w.verifications(t, impl)) != 1 {
		t.Fatalf("an old attempt posing as round 2: %q (%v)", did, concludeErr)
	}
}

// Review 152 A2: when the only check of a subject under verification is
// closed, a pinned verification takes its place; with another check open,
// none is added.
func TestClosedOnlyCheckIsReplaced(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	first := w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	unpinned := w.handCheck(t, "verify", impl, first.AttemptID, 1)
	if c := w.run(t, "h2", deterministic, "done"); c.TaskID != v {
		t.Fatalf("took %s", c.TaskID)
	}
	// v is submitted, still open: closing the unpinned one adds nothing.
	if c := w.run(t, "h3", deterministic, "done"); c.TaskID != unpinned {
		t.Fatalf("took %s", c.TaskID)
	}
	did, concludeErr := store.Conclude(ctx, w.rt, unpinned)
	if concludeErr != nil || did != "closed" {
		t.Fatalf("%q (%v)", did, concludeErr)
	}
	var checks int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE subject_task_id = $1 AND role = 'verify'`, impl).Scan(&checks))
	if checks != 2 {
		t.Fatalf("%d verification tasks; none should have been added", checks)
	}

	w2 := newKernelWorld(t)
	impl2 := w2.ready(t, store.Task{})
	a := w2.run(t, "h1", cursor, "done")
	v2, err := store.StartVerification(ctx, w2.rt, impl2)
	ok(t, err)
	ok(t, store.InTx(ctx, w2.rt, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", v2, "cancelled", "test") }))
	lone := w2.handCheck(t, "verify", impl2, a.AttemptID, 1)
	w2.run(t, "h2", deterministic, "done")
	did, concludeErr = store.Conclude(ctx, w2.rt, lone)
	if concludeErr != nil || did != "closed" {
		t.Fatalf("%q (%v)", did, concludeErr)
	}
	replacement := w2.checker(t, impl2, "verify")
	if pin := w2.checkSpec(t, replacement); pin["attempt"] != a.AttemptID {
		t.Fatalf("the replacement pins %v", pin)
	}
}
