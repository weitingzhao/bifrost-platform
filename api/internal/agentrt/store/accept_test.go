package store_test

import (
	"sync"
	"testing"

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
	method, verdict, attempt, verifier string
	accepts                            bool
}

func (w *world) verifications(t *testing.T, task string) []verificationRow {
	t.Helper()
	rows, err := w.rt.Query(ctx, `SELECT method, verdict, attempt_id, COALESCE(verifier_attempt_id, ''), accepts_attempt
	  FROM agentrt.verification WHERE task_id = $1 ORDER BY created_at, verification_id`, task)
	ok(t, err)
	defer rows.Close()
	var out []verificationRow
	for rows.Next() {
		var v verificationRow
		ok(t, rows.Scan(&v.method, &v.verdict, &v.attempt, &v.verifier, &v.accepts))
		out = append(out, v)
	}
	ok(t, rows.Err())
	return out
}

// v5 §5.2 「验收编排」: a submitted implementation is verified
// deterministically, then reviewed, then accepted; each check is a task of
// its own, accepted once its result is used.
func TestAcceptanceFlow(t *testing.T) {
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	ic := w.run(t, "h1", cursor, "done")

	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	if v == "" || w.state(t, "task", impl) != "verifying" || w.state(t, "task", v) != "ready" {
		t.Fatalf("verification task %q; implementation %s", v, w.state(t, "task", impl))
	}
	var spec, implSpec string
	ok(t, w.rt.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE task_id = $1`, v).Scan(&spec))
	ok(t, w.rt.QueryRow(ctx, `SELECT spec_artifact_id FROM agentrt.task WHERE task_id = $1`, impl).Scan(&implSpec))
	if spec != implSpec {
		t.Fatal("the verification does not carry the implementation's task spec")
	}
	again, err := store.StartVerification(ctx, w.rt, impl)
	if err != nil || again != "" {
		t.Fatalf("a second start made %q (%v)", again, err)
	}

	vc := w.run(t, "h2", deterministic, "done")
	if vc.TaskID != v {
		t.Fatalf("the deterministic executor took %s", vc.TaskID)
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
	if rc.TaskID != review {
		t.Fatalf("codex took %s", rc.TaskID)
	}
	did, err = store.Conclude(ctx, w.rt, review)
	ok(t, err)
	if did != "accepted" || w.state(t, "task", impl) != "accepted" || w.state(t, "task", review) != "accepted" {
		t.Fatalf("conclude did %q; implementation %s", did, w.state(t, "task", impl))
	}
	got := w.verifications(t, impl)
	want := []verificationRow{{"deterministic", "pass", ic.AttemptID, vc.AttemptID, false}, {"review", "pass", ic.AttemptID, rc.AttemptID, true}}
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

// A failed check sends the implementation back while rounds remain, then
// blocks it; nothing is accepted.
func TestAcceptanceRework(t *testing.T) {
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
