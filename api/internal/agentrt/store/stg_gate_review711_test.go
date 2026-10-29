package store

import (
	"context"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// Review 711 regressions: each negative has a same-entry success control.
func review711Disposed(t *testing.T) *stgPolicyFixture {
	t.Helper()
	x := stgPolicy(t)
	if d := x.decide(false); !d.Allowed {
		t.Fatal("positive control", d)
	}
	old := stgTestMember2()
	old["R"], old["op"] = stgSHA("a"), "land:original:1"
	oldKey := stgMember(old)
	// Produce a disposed original through Parse -> Append check -> Fold,
	// using the same legal history as the existing disposition edge test.
	h := stgReady(t, old)
	h.landed(oldKey.Op, oldKey.Root, oldKey.R)
	h.raw(map[string]any{"text": "reversal", "landing": stgLegacyRecord(oldKey.Op, "reverted", "R", oldKey.R, "by", stgSHA("b"))})
	ref := int64(len(h.entries))
	beforeReversal := len(h.entries)
	h.landed("land:one:1", stgTestRoot, stgSHA("b"))
	// The existing helper always uses e as M0. Make the reversal's recorded
	// parent match the complete Git chain e -> a -> b, then validate replay.
	for i := beforeReversal; i < len(h.entries); i++ {
		r := h.entries[i].Event.Body["landing"].(map[string]any)
		if _, exists := r["M0"]; exists {
			r["M0"] = oldKey.R
		}
	}
	_ = h.state()
	h.add(memberRecord(old, "disposed", ref))
	h.settle(old)
	// The ledger refuses putting the disposed original in a later batch.
	h.reject(stgForBatch(h.opened(old), 2), "transition")
	current := stgTestMember()
	current["R"] = stgSHA("b")
	h.add(stgForBatch(h.opened(current), 2))
	h.add(stgForBatch(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)), 2))
	ci := h.ci("new-ci")
	ci["ci"].(map[string]any)["repo_url"] = landing.LandingRepos["bifrost-platform"].Remote
	h.add(stgForBatch(ci, 2))
	h.add(stgForBatch(h.snapshot(1, current), 2))
	x.s, x.at = h.state(), h.at
	x.o.ReadAt, x.o.CI.ReadAt = x.at, x.at
	x.o.CI.Accepted = stgClone(ci["ci"].(map[string]any))
	x.o.CI.UID = "new-ci"
	x.o.CI.Accepted["read_at"] = x.at.Format(time.RFC3339Nano)
	x.o.Lease.Who = "stgrel-bifrost-platform-2-1"
	x.o.Lease.Expires = x.at.Add(5 * time.Minute)
	x.f.Bindings[oldKey.Root] = stgBinding{Root: oldKey.Root, Tenant: "dev", Repo: "bifrost-platform", Mission: "M-original"}
	x.f.Authority[oldKey.Op] = stgLandingAuthority{RuleVersion: 1}
	first := x.o.Commits[0]
	first.SHA = oldKey.R
	first.Message = "Original\n\nAgentrt-Task: " + oldKey.Root + "\nLanded-By: agentrt-prod/" + oldKey.Op + "\n"
	x.o.Commits[0].Parents = []string{oldKey.R}
	x.o.Commits = append([]landing.ReleaseCommit{first}, x.o.Commits...)
	x.h = h
	return x
}

func TestReview711DisposedReversalMustNotReenterBudget(t *testing.T) {
	t.Parallel()
	x := review711Disposed(t)
	decide := func() StgGateDecision { return stgEvaluateGate(x.s, "stg:bifrost-platform:2", x.o, x.f, x.at, false) }
	d := decide()
	if !d.Allowed || len(d.Set) != 1 || d.Set[0].R != stgSHA("b") {
		t.Fatalf("authorized original+reversal, with original legally disposed, cannot proceed: %+v", d)
	}
	// The same history must still authorize both commits, even though only
	// the reversal is charged. Neither human reverts nor missing provenance
	// are hidden by the original's terminal disposition.
	original := x.o.Commits[1].Message
	x.o.Commits[1].Message = "Manual revert"
	if d = decide(); d.Allowed || d.Why != "diff_unlanded" {
		t.Fatal("manual reversal accepted", d)
	}
	x.o.Commits[1].Message = original
	for _, op := range []string{"land:original:1", "land:one:1"} {
		a := x.f.Authority[op]
		delete(x.f.Authority, op)
		if d = decide(); d.Allowed || d.Why != "rule" {
			t.Fatal("disposed range skipped authorization", op, d)
		}
		x.f.Authority[op] = a
	}
	old := StgMemberKey{Root: stgTestRoot2, Op: "land:original:1", R: stgSHA("a")}
	ref, _ := cardNumber(x.s.Members[old].Ref)
	for _, by := range []string{stgSHA("f"), old.R} {
		x.s.Records[ref].Record["by"] = by
		if d = decide(); d.Allowed || d.Why != "diff_unlanded" {
			t.Fatal("reversal absent from later authorized range", by, d)
		}
	}
}

// Serial: FreezeEventClock. Real JSONB readback and Append's debit calculation,
// fixed to the replay. Policy facts are the same trusted fixture as above.
func TestReview711PGDisposedReversalMatchesReplay(t *testing.T) {
	_, conn := stgPG(t)
	x := review711Disposed(t)
	stgPGReplay(t, conn, x.h.entries)
	x.s = stgPGRead(t, conn)
	defer FreezeEventClock(x.at)()
	d := stgEvaluateGate(x.s, "stg:bifrost-platform:2", x.o, x.f, x.at, false)
	if !d.Allowed || len(d.Set) != 1 || d.Set[0].R != stgSHA("b") {
		t.Fatal("stored disposed range cannot proceed", d)
	}
	_, err := stgPGAppend(conn, stgNote(stgForBatch(x.h.start(1), 2)))
	if err != nil {
		t.Fatal(err)
	}
	s := stgPGRead(t, conn)
	old := StgMemberKey{Root: stgTestRoot2, Op: "land:original:1", R: stgSHA("a")}
	if s.Budget[StgBudgetKey{stgSHA("a"), 1}] != 0 || s.Budget[StgBudgetKey{stgSHA("b"), 1}] != 1 || s.Members[old].To != "disposed" {
		t.Fatal("disposed member revived or charged", s.Budget, s.Members[old])
	}
	if _, err = VerifyStream(context.Background(), conn, StgReleaseStream); err != nil {
		t.Fatal(err)
	}
}

func TestReview711UnknownCIWithoutResultsFailureMustWait(t *testing.T) {
	t.Parallel()
	x := stgPolicy(t)
	if d := x.decide(false); !d.Allowed {
		t.Fatal("positive control", d)
	}
	// A just-created PipelineRun with no status has Unknown and no reason.
	// This is different from a terminal Succeeded run with malformed results.
	x.o.CI.Status, x.o.CI.Reason, x.o.CI.Accepted = "Unknown", "", nil
	d := x.decide(false)
	if d.Why != "ci_waiting" {
		h := x.h
		ci := h.ci("just-created")["ci"].(map[string]any)
		base := stgTestBase()
		id := StgBlockID(stgSHA("b"), base["P"].(string), "ci_results", "v1")
		block := h.add(stgRecord("blocked", "block", id, "why", "ci_results", "P", stgSHA("b"), "base", base, "first_at", h.at.Format(time.RFC3339Nano), "evidence_version", "v1", "detail", map[string]any{"ci": map[string]any{"uid": ci["uid"], "created_at": ci["created_at"], "read_at": ci["read_at"]}}))
		ci["read_at"] = h.at.Format(time.RFC3339Nano)
		_, _, err := h.check(stgNote(stgRecord("ci_observed", "block", id, "interval_seq", block.Seq, "P", stgSHA("b"), "ci", ci, "status", "Succeeded")))
		t.Fatalf("transient unknown classified as durable failure: %+v; same UID succeeding cannot repair: %v", d, err)
	}
	accepted := stgClone(x.h.ci(x.o.CI.UID)["ci"].(map[string]any))
	accepted["repo_url"] = landing.LandingRepos["bifrost-platform"].Remote
	accepted["read_at"] = x.at.Format(time.RFC3339Nano)
	x.o.CI.Status = "Running"
	if d = x.decide(false); d.Allowed || d.Why != "ci_waiting" {
		t.Fatal("same UID running", d)
	}
	x.o.CI.Status, x.o.CI.Accepted = "Succeeded", accepted
	if d = x.decide(false); !d.Allowed {
		t.Fatal("same UID succeeded", d)
	}
	for _, reason := range []string{"ci_results", "ci_identity", "ci_tie"} {
		x.o.CI.Status, x.o.CI.Reason, x.o.CI.Accepted = "Unknown", reason, nil
		if d = x.decide(false); d.Allowed || d.Why != "ci_results" {
			t.Fatal("definite error treated as waiting", reason, d)
		}
	}
}

func TestReview711InFlightCreateMustNotReleaseSlot(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	tr := h.state().Batches[stgTestBatch].Tries[1]
	at := tr.Permit.Record.Time("deadline").Add(time.Millisecond)
	obs := StgRunObservation{Kind: "found", UID: "late-run", IntentSeq: tr.Started.Seq, PermitSeq: tr.Permit.Seq, ReadAt: at, FinalizerGuaranteed: true}
	if got := StgRecoveryDecision(tr, obs, at); got != "bound" {
		t.Fatal("same-entry found control", got)
	}
	obs.Kind, obs.UID = "not_found", ""
	if got := StgRecoveryDecision(tr, obs, at); got != "unknown_execution" {
		t.Fatalf("POST may still complete after deadline: got %s, want isolation", got)
	}
	h.at = at
	h.add(stgRecord("quarantined", "try", 1, "why", "unknown_execution", "at", at.Format(time.RFC3339Nano)))
	h.reject(stgRecord("unsent", "try", 1, "proof", "gate_failed_before_send"), "transition")
	tr = h.state().Batches[stgTestBatch].Tries[1]
	obs.Kind, obs.UID, obs.ReadAt = "found", "late-run", h.at
	if got := StgRecoveryDecision(tr, obs, h.at); got != "bound" || !tr.Occupied() {
		t.Fatal("late UID cannot be taken over", got, tr)
	}
	h.add(stgRecord("bound", "try", 1, "uid", "late-run"))
	h.reject(memberRecord(stgTestMember(), "released", tr.Quarantined.Seq), "transition")
	h.add(stgRecord("run_evidence", "try", 1, "uid", "late-run", "status", "Succeeded", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", h.at.Format(time.RFC3339Nano)))
	tr = h.state().Batches[stgTestBatch].Tries[1]
	if tr.Occupied() || tr.IsQuarantined() || tr.Unsent.Seq != 0 || h.state().Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 {
		t.Fatal("terminal readback did not reconcile without refund", tr)
	}
}
