package store_test

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Serial: FreezeEventClock and real transactions. The pause is AFTER the
// final permit check, inside Create and before persistence. The adapter's
// actual POST boundary has an independent fake-Kubernetes regression.
func TestReview711PGInFlightCreateAndLateReadback(t *testing.T) {
	for _, mode := range []string{"success", "none", "live off", "none lost response"} {
		t.Run(mode, func(t *testing.T) {
			x := b5Ready(t)
			runs := b5RunAdapter(t)
			sender := stgrelease.Sender{DB: x.w.rt, Runs: runs, Gate: x.src, FinalizerGuaranteed: true}
			runs.before = func() { x.advance(x.at.Add(2*time.Minute - time.Millisecond)) }
			runs.lost = mode == "none lost response"
			if mode != "success" {
				runs.afterCheck = func() {
					x.advance(x.at.Add(2*time.Minute + time.Millisecond))
					if mode == "live off" {
						sender.Gate.Live = func() bool { return false }
					} else {
						x.none(t)
					}
					if runs.obj != nil {
						t.Fatal("pause must precede persistence")
					}
					action, err := sender.Recover(ctx, b5Batch, 1)
					ok(t, err)
					s := x.read(t)
					tr := s.Batches[b5Batch].Tries[1]
					if action != "unknown_execution" || !tr.Occupied() || !tr.IsQuarantined() || tr.Unsent.Seq != 0 || tr.Permit.Seq != tr.Started.Seq || runs.creates != 0 {
						t.Fatal("in-flight POST released or resent", action, tr, runs.creates)
					}
					seq := s.LastSeq
					action, err = sender.Recover(ctx, b5Batch, 1)
					ok(t, err)
					if action != "wait" || x.read(t).LastSeq != seq {
						t.Fatal("isolation not idempotent", action)
					}
				}
			}
			d, err := sender.Start(ctx, b5Batch, 1)
			ok(t, err)
			s := x.read(t)
			tr := s.Batches[b5Batch].Tries[1]
			if !d.Allowed || runs.creates != 1 || tr.Bound.Record.String("uid") != "uid-one" || !tr.Occupied() || tr.Unsent.Seq != 0 || s.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 {
				t.Fatal("late UID not bound with one debit", d, tr, runs.creates, s.Budget)
			}
			// A restarted observer can still read the quarantined matching UID.
			action, err := sender.Recover(ctx, b5Batch, 1)
			ok(t, err)
			if action != "bound" || runs.creates != 1 {
				t.Fatal("late readback failed", action, runs.creates)
			}
			b5Append(t, x.w, b5Record("run_evidence", "batch", "try", 1, "uid", "uid-one", "status", "Succeeded", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", store.DecisionAt().Format(time.RFC3339Nano)))
			tr = x.read(t).Batches[b5Batch].Tries[1]
			if tr.Occupied() || tr.IsQuarantined() || tr.RunEvidence.Seq == 0 {
				t.Fatal("terminal evidence did not reconcile isolation", tr)
			}
		})
	}
}

// Serial: FreezeEventClock. The public gate leaves no interval/debit while
// the new UID is nonterminal, then accept that very UID when it succeeds.
func TestReview711PGUnknownCIProgressesWithSameUID(t *testing.T) {
	t.Run("same-entry success", func(t *testing.T) { b5Ready(t).permit(t) })
	x := b5Ready(t)
	accepted := x.obs.CI.Accepted
	accepted["uid"], accepted["name"] = "new-ci-uid", "new-ci"
	x.obs.CI.UID = "new-ci-uid"
	x.obs.CI.CreatedAt = x.at.Add(-time.Minute)
	x.obs.CI.Accepted = nil
	before := x.read(t).LastSeq
	for _, status := range []string{"Unknown", "Running"} {
		x.obs.CI.Status = status
		_, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
		ok(t, err)
		s := x.read(t)
		if d.Allowed || d.Why != "ci_waiting" || d.Block != "" || len(s.BlockHistory) != 0 || len(s.Budget) != 0 || s.LastSeq != before {
			t.Fatal("nonterminal CI created a durable refusal", status, d, s.BlockHistory)
		}
	}
	x.obs.CI.Status, x.obs.CI.Accepted = "Succeeded", accepted
	x.permit(t)
	s := x.read(t)
	if s.Batches[b5Batch].CI["new-ci-uid"].Seq == 0 || len(s.BlockHistory) != 0 || s.Budget[store.StgBudgetKey{R: commitB, Gen: 1}] != 1 {
		t.Fatal("same UID success was not accepted exactly once", s.Budget, s.BlockHistory)
	}
}

// Serial: FreezeEventClock. Definite reader errors persist ci_results; they must not acquire
// the transient Unknown exemption.
func TestReview711PGCIResultsErrorsStillBlock(t *testing.T) {
	for _, reason := range []string{"ci_results", "ci_identity", "ci_tie"} {
		t.Run(reason, func(t *testing.T) {
			x := b5Ready(t)
			x.obs.CI.Status, x.obs.CI.Reason, x.obs.CI.Accepted = "Unknown", reason, nil
			_, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
			ok(t, err)
			s := x.read(t)
			if d.Allowed || d.Why != "ci_results" || d.Block == "" || len(s.BlockHistory) != 1 || len(s.Budget) != 0 {
				t.Fatal("definite CI failure lost its interval", d)
			}
		})
	}
}
