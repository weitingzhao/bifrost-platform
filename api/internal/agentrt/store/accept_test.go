package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var deterministic = store.Tool{Executor: "exec:deterministic", Provider: "local"}

// run claims the next task tool may take on host and carries it through to
// submit with the given conclusion; a branch task pushes its own branch.
func (w *world) run(t *testing.T, host string, tool store.Tool, conclusion string) *store.Claim {
	t.Helper()
	c := w.claim(t, host, store.LeaseTTL, tool)
	if c == nil {
		t.Fatalf("%s on %s found nothing to claim", tool.Executor, host)
	}
	f := fence(c, host)
	ok(t, store.Start(ctx, w.rt, f, "", ""))
	exit := 0
	if conclusion != "done" {
		exit = 1
	}
	_, err := store.Computed(ctx, w.rt, f, &exit, "exit", conclusion)
	ok(t, err)
	r := store.Result{Report: w.artifact(t, "report", host+conclusion+c.TaskID)}
	if c.Effect == "branch" {
		r.Ref, r.Commit = c.PushedRef, commitA
	}
	calls := 0
	_, err = store.Persist(ctx, w.rt, f, r, heads(map[string]string{c.PushedRef: commitA}, &calls))
	ok(t, err)
	_, err = store.Submit(ctx, w.rt, f)
	ok(t, err)
	return c
}

func (w *world) checker(t *testing.T, subject, role string) string {
	t.Helper()
	var id string
	ok(t, w.rt.QueryRow(ctx, `SELECT task_id FROM agentrt.task WHERE subject_task_id = $1 AND role = $2 AND state = 'ready'`,
		subject, role).Scan(&id))
	return id
}

type verificationRow struct {
	method, verdict, attempt, verifier, commit string
	accepts                                    bool
}

// checkSpec reads the spec of a verification or review task.
func (w *world) checkSpec(t *testing.T, task string) map[string]any {
	t.Helper()
	var doc map[string]any
	ok(t, w.rt.QueryRow(ctx, `SELECT convert_from(b.body, 'UTF8')::jsonb FROM agentrt.task t
	  JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id WHERE t.task_id = $1`, task).Scan(&doc))
	return doc
}

func (w *world) verifications(t *testing.T, task string) []verificationRow {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT method, verdict, attempt_id, COALESCE(verifier_attempt_id, ''), COALESCE(subject_commit, ''),
	  accepts_attempt FROM agentrt.verification WHERE task_id = $1 ORDER BY created_at, verification_id`, task)
	ok(t, err)
	defer rows.Close()
	var out []verificationRow
	for rows.Next() {
		var v verificationRow
		ok(t, rows.Scan(&v.method, &v.verdict, &v.attempt, &v.verifier, &v.commit, &v.accepts))
		out = append(out, v)
	}
	ok(t, rows.Err())
	return out
}

// v5 §5.2 「验收编排」: a submitted implementation is verified
// deterministically, then reviewed, then accepted; each check is a task of
// its own, accepted once its result is used.
func TestAcceptanceFlow(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{Repo: "bifrost-platform"})
	ic := w.run(t, "h1", cursor, "done")

	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if v == "" || w.state(t, "task", impl) != "verifying" || w.state(t, "task", v) != "ready" {
		t.Fatalf("verification task %q; implementation %s", v, w.state(t, "task", impl))
	}
	var implSpec string
	ok(t, w.rt.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE task_id = $1`, impl).Scan(&implSpec))
	pin := w.checkSpec(t, v)
	if pin["check"] != "verify" || pin["subject_task"] != impl || pin["attempt"] != ic.AttemptID || pin["commit"] != commitA ||
		pin["task_spec"] != implSpec || fmt.Sprint(pin["round"]) != "1" || pin["repo"] != "bifrost-platform" {
		t.Fatalf("the verification's spec pins %v", pin)
	}
	again, err := store.StartVerification(ctx, w.rt, impl)
	if err != nil || again != "" {
		t.Fatalf("a second start made %q (%v)", again, err)
	}

	vc := w.run(t, "h2", deterministic, "done")
	if vc.TaskID != v || vc.Repo != "bifrost-platform" || vc.BaseCommit != commitA {
		t.Fatalf("the verification's claim = %+v; want the subject's repository at its commit", vc)
	}
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	if did != "review" || w.state(t, "task", v) != "accepted" || w.state(t, "task", impl) != "verifying" {
		t.Fatalf("conclude did %q", did)
	}
	if did, err = store.Conclude(ctx, w.rt, v); err != nil || did != "" {
		t.Fatalf("a second conclude did %q (%v)", did, err)
	}
	review := w.checker(t, impl, "review")

	rc := w.run(t, "h3", codex, "done")
	if rc.TaskID != review || rc.Repo != "bifrost-platform" || rc.BaseCommit != commitA || w.checkSpec(t, review)["attempt"] != ic.AttemptID {
		t.Fatalf("review claim = %+v", rc)
	}
	did, err = store.Conclude(ctx, w.rt, review)
	ok(t, err)
	if did != "accepted" || w.state(t, "task", impl) != "accepted" || w.state(t, "task", review) != "accepted" {
		t.Fatalf("conclude did %q; implementation %s", did, w.state(t, "task", impl))
	}
	got := w.verifications(t, impl)
	want := []verificationRow{{"deterministic", "pass", ic.AttemptID, vc.AttemptID, commitA, false},
		{"review", "pass", ic.AttemptID, rc.AttemptID, commitA, true}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("verifications = %+v", got)
	}
	var recorded int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'verification_recorded' AND task_id = $1`, impl).Scan(&recorded))
	if recorded != 2 {
		t.Fatalf("%d verification_recorded events", recorded)
	}
	w.chainHolds(t, "mission:M-1")
}

// W-31 2.4 「Claude 写核心代码」: core work goes the whole way. Only claude
// (only author_core) implements it; agentd's deterministic check verifies it
// (before, its verification needed only review_core, which no verifier
// holds, and stayed ready for ever); only codex (only review_core) reviews
// it; it is accepted.
func TestCoreWorkIsAccepted(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{Repo: "bifrost-platform", Core: true})
	if c := w.claim(t, "h1", store.LeaseTTL, codex, cursor); c != nil {
		t.Fatalf("core work went to %s", c.Tool.Executor)
	}
	ic := w.run(t, "h1", claude, "done")
	if ic.TaskID != impl {
		t.Fatalf("claude claimed %s", ic.TaskID)
	}
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if c := w.claim(t, "h2", store.LeaseTTL, claude, cursor); c != nil {
		t.Fatalf("the verification of core work went to %s", c.Tool.Executor)
	}
	vc := w.run(t, "h2", deterministic, "done")
	if vc.TaskID != v {
		t.Fatalf("the check claimed %s, not the verification %s", vc.TaskID, v)
	}
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	if did != "review" {
		t.Fatalf("conclude did %q", did)
	}
	review := w.checker(t, impl, "review")
	if c := w.claim(t, "h3", store.LeaseTTL, claude, cursor, deterministic); c != nil {
		t.Fatalf("the review of core work went to %s", c.Tool.Executor)
	}
	rc := w.run(t, "h3", codex, "done")
	if rc.TaskID != review {
		t.Fatalf("codex claimed %s, not the review %s", rc.TaskID, review)
	}
	if did, err = store.Conclude(ctx, w.rt, review); err != nil || did != "accepted" || w.state(t, "task", impl) != "accepted" {
		t.Fatalf("conclude did %q (%v); implementation %s", did, err, w.state(t, "task", impl))
	}
}

// Review 422 A1: a core verification made ready by the build before, whose
// last route_decided (algorithm 2) sent it to nobody, is decided again once
// this build runs (its algorithm differs, so its fingerprint does), and the
// deterministic check claims it. Had the checks changed without the
// algorithm, its fingerprint would match and it would stay unclaimed.
// The verifier job has the deterministic check alone, as the STG and PROD
// rules give it; the build before decided that input as staged here
// (review 423 B1). Reroute reads a task's last route_decided only, so the
// event this build wrote before it does not count.
func TestCoreVerificationRoutedBeforeIsRoutedAgain(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: the verifier job as the rules give it"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.job_candidate SET status = 'retired'
		  WHERE job_id = 'verifier' AND executor_actor_id <> 'exec:deterministic'`)
		return err
	}))
	const check = "verifier/exec:deterministic/local/*"
	impl := w.ready(t, store.Task{Repo: "bifrost-platform", Core: true})
	w.run(t, "h1", claude, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	fingerprint := func(rec map[string]any) string {
		doc, eerr := canon.Encode(rec)
		ok(t, eerr)
		sum := sha256.Sum256(doc)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	var fp, inputs, route string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'fingerprint', (body_json->'inputs')::text, (body_json->'candidates')::text
	  FROM agentrt.event WHERE task_id = $1 AND event_type = 'route_decided' ORDER BY event_id DESC LIMIT 1`, v).Scan(&fp, &inputs, &route))
	if route != `["`+check+`"]` {
		t.Fatalf("this build routes the core verification to %s, want the check alone", route)
	}
	doc, err := canon.Parse([]byte(inputs))
	ok(t, err)
	rec := doc.(map[string]any)
	if got := fingerprint(rec); got != fp {
		t.Fatalf("the recorded fingerprint %s is not its input's %s", fp, got)
	}
	// The route as the build before decided it: the same input under
	// algorithm 2; the check, its one candidate, excluded for want of
	// review_core, with the reasons that build gave (04e815f6^ route.go).
	rec["algorithm"] = json.Number("2")
	excluded := []any{map[string]any{"candidate": check, "reason": "a need only others may take",
		"reasons": []any{"a need only others may take", "core work needs only review_core"}}}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, aerr := store.Append(ctx, tx, store.Event{Stream: store.MissionStream("M-1"), Type: "route_decided", Actor: "controller",
			MissionID: "M-1", PlanNode: s1, TaskID: v, Body: map[string]any{"job": "verifier", "candidates": []any{}, "excluded": excluded,
				"inputs": rec, "fingerprint": fingerprint(rec)}}); aerr != nil {
			return aerr
		}
		_, xerr := tx.Exec(ctx, `UPDATE agentrt.task SET route_json = '[]'::jsonb WHERE task_id = $1`, v)
		return xerr
	}))
	if c := w.claim(t, "h2", store.LeaseTTL, deterministic); c != nil {
		t.Fatalf("%s was claimed on an empty route", c.TaskID)
	}
	n, err := store.Reroute(ctx, w.rt)
	ok(t, err)
	if n != 1 {
		t.Fatalf("Reroute decided %d routes, want the stale verification's", n)
	}
	if c := w.claim(t, "h2", store.LeaseTTL, deterministic); c == nil || c.TaskID != v {
		t.Fatalf("the check's claim after the reroute = %+v; want %s", c, v)
	}
}

// A failed check sends the implementation back while rounds remain, then
// blocks it; nothing is accepted.
func TestAcceptanceRework(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "failed")
	did, err := store.Conclude(ctx, w.rt, v)
	ok(t, err)
	var round int
	ok(t, w.rt.QueryRow(ctx, `SELECT round_no FROM agentrt.task WHERE task_id = $1`, impl).Scan(&round))
	if did != "rework" || w.state(t, "task", impl) != "ready" || round != 2 {
		t.Fatalf("conclude did %q; implementation %s in round %d", did, w.state(t, "task", impl), round)
	}

	second := w.run(t, "h3", cursor, "done")
	if second.TaskID != impl || second.AttemptNo != 2 {
		t.Fatalf("round 2 claim = %+v", second)
	}
	v2, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h4", deterministic, "done")
	if did, err = store.Conclude(ctx, w.rt, v2); err != nil || did != "review" {
		t.Fatalf("round 2 verification: %q (%v)", did, err)
	}
	review := w.checker(t, impl, "review")
	w.run(t, "h5", codex, "failed")
	if did, err = store.Conclude(ctx, w.rt, review); err != nil || did != "blocked" || w.state(t, "task", impl) != "blocked" {
		t.Fatalf("a failed review in the last round: %q (%v), implementation %s", did, err, w.state(t, "task", impl))
	}
	for _, v := range w.verifications(t, impl) {
		if v.accepts {
			t.Fatal("a failed round accepted an attempt")
		}
	}
	if n := len(w.verifications(t, impl)); n != 3 {
		t.Fatalf("%d verifications; want fail, pass, review fail", n)
	}
}

// Two controllers using one result at once use it once.
func TestConcludeOnce(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	impl := w.ready(t, store.Task{})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "done")
	var wg sync.WaitGroup
	dids := make([]string, 2)
	errs := make([]error, 2)
	for i := range dids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dids[i], errs[i] = store.Conclude(ctx, pool, v)
		}(i)
	}
	wg.Wait()
	ok(t, errs[0])
	ok(t, errs[1])
	if (dids[0] == "review") == (dids[1] == "review") || dids[0]+dids[1] != "review" {
		t.Fatalf("conclude results %q", dids)
	}
	var reviews int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE subject_task_id = $1 AND role = 'review'`, impl).Scan(&reviews))
	if reviews != 1 || len(w.verifications(t, impl)) != 1 {
		t.Fatalf("%d review tasks, %d verifications", reviews, len(w.verifications(t, impl)))
	}
}

// T26 (the Owner 2026-10-18): on STG a task carrying the test need fails its
// first deterministic verification whatever it found, recorded as injected,
// and goes round again; its second round is verified as found. Without the
// switch (PROD) the need does nothing.
func TestFailFirstVerifySwitch(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	on := store.ConcludeOptions{FailFirstVerify: true}
	impl := w.ready(t, store.Task{Needs: []string{store.TestNeedFailFirstVerify}})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "done")
	did, err := store.ConcludeWith(ctx, w.rt, v, on)
	ok(t, err)
	if did != "rework" || w.state(t, "task", impl) != "ready" {
		t.Fatalf("round 1 with the switch: %q, implementation %s", did, w.state(t, "task", impl))
	}
	if got := w.verifications(t, impl); len(got) != 1 || got[0].verdict != "fail" {
		t.Fatalf("verifications = %+v", got)
	}
	var injected bool
	var reason string
	ok(t, w.rt.QueryRow(ctx, `SELECT (body_json->>'injected')::boolean FROM agentrt.event WHERE event_type = 'verification_recorded' AND task_id = $1`,
		impl).Scan(&injected))
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'reason' FROM agentrt.event WHERE event_type = 'task_state_changed' AND task_id = $1
	  AND body_json->>'to' = 'rework'`, impl).Scan(&reason))
	if !injected || !strings.Contains(reason, "STG test switch") {
		t.Fatalf("injected %v, rework reason %q", injected, reason)
	}
	var rowRef, rowEnv string
	ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(check_ref, ''), COALESCE(env, '') FROM agentrt.verification WHERE task_id = $1`, impl).Scan(&rowRef, &rowEnv))
	if rowRef != store.TestNeedFailFirstVerify || rowEnv != "stg" {
		t.Fatalf("the verification row says %q %q; want the switch and stg", rowRef, rowEnv)
	}
	w.run(t, "h3", cursor, "done")
	v2, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h4", deterministic, "done")
	if did, err = store.ConcludeWith(ctx, w.rt, v2, on); err != nil || did != "review" {
		t.Fatalf("round 2 with the switch: %q (%v)", did, err)
	}

	// Without the switch the need is just a need.
	w2 := newKernelWorld(t)
	other := w2.ready(t, store.Task{Needs: []string{store.TestNeedFailFirstVerify}})
	w2.run(t, "h1", cursor, "done")
	v3, err := store.StartVerification(ctx, w2.rt, other)
	ok(t, err)
	w2.run(t, "h2", deterministic, "done")
	if did, err = store.Conclude(ctx, w2.rt, v3); err != nil || did != "review" {
		t.Fatalf("without the switch: %q (%v)", did, err)
	}
}
