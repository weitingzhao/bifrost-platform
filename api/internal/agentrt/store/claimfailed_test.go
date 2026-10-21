package store_test

import (
	"fmt"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// evidence is the report of the task's verification of method on attempt.
func (w *world) evidence(t *testing.T, task, method, attempt string) string {
	t.Helper()
	var id string
	ok(t, w.rt.QueryRow(ctx, `SELECT evidence_artifact_id FROM agentrt.verification WHERE task_id = $1 AND method = $2 AND attempt_id = $3`,
		task, method, attempt).Scan(&id))
	return id
}

// TD-405 (PROD M-23): the claim of an implementation in a round after the
// first carries the report of the check that sent its task round again:
// round 2, the deterministic check that failed round 1. The first round's
// claim and a check's claim carry none.
func TestRetriedImplementationClaimCarriesTheFailedCheck(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	first := w.run(t, "h1", cursor, "done")
	noFailedChecks(t, "round 1's claim", first)
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	noFailedChecks(t, "a check's claim", w.run(t, "h2", deterministic, "failed"))
	concludeAs(t, w, v, "rework")

	second := w.run(t, "h3", cursor, "done")
	want := []store.FailedCheck{{Method: "deterministic", AttemptNo: 1, Evidence: w.evidence(t, impl, "deterministic", first.AttemptID)}}
	if second.AttemptNo != 2 || fmt.Sprint(second.Failed) != fmt.Sprint(want) {
		t.Fatalf("round 2's claim: attempt %d carries %+v, want %+v", second.AttemptNo, second.Failed, want)
	}
}

// A round that passed its deterministic check and failed its review: the
// next round is told of the review only, not of the check that passed.
func TestRetriedImplementationClaimCarriesTheFailedReview(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{})
	first := w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "done")
	concludeAs(t, w, v, "review")
	review := w.checker(t, impl, "review")
	noFailedChecks(t, "a review's claim", w.run(t, "h3", codex, "failed"))
	concludeAs(t, w, review, "rework")

	second := w.run(t, "h4", cursor, "done")
	want := []store.FailedCheck{{Method: "review", AttemptNo: 1, Evidence: w.evidence(t, impl, "review", first.AttemptID)}}
	if fmt.Sprint(second.Failed) != fmt.Sprint(want) {
		t.Fatalf("round 2's claim carries %+v, want the review only: %+v", second.Failed, want)
	}
}

func noFailedChecks(t *testing.T, what string, c *store.Claim) {
	t.Helper()
	if len(c.Failed) != 0 {
		t.Fatalf("%s carries %+v", what, c.Failed)
	}
}

func concludeAs(t *testing.T, w *world, check, want string) {
	t.Helper()
	if did, err := store.Conclude(ctx, w.rt, check); err != nil || did != want {
		t.Fatalf("conclude did %q (%v), want %q", did, err, want)
	}
}

// A failure the STG test switch injected says nothing of the work: the next
// round is told of none.
func TestRetriedClaimLeavesOutAnInjectedFailure(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	impl := w.ready(t, store.Task{Needs: []string{store.TestNeedFailFirstVerify}})
	w.run(t, "h1", cursor, "done")
	v, err := store.StartVerification(ctx, w.rt, impl)
	ok(t, err)
	w.run(t, "h2", deterministic, "done")
	if did, cerr := store.ConcludeWith(ctx, w.rt, v, store.ConcludeOptions{FailFirstVerify: true}); cerr != nil || did != "rework" {
		t.Fatalf("round 1 with the switch: %q (%v)", did, cerr)
	}
	if c := w.run(t, "h3", cursor, "done"); c.AttemptNo != 2 || len(c.Failed) != 0 {
		t.Fatalf("round 2's claim: attempt %d carries %+v", c.AttemptNo, c.Failed)
	}
}
