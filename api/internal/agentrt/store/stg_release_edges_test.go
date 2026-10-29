package store

import (
	"fmt"
	"testing"
	"time"
)

func TestStgVocabularyUsesTheClosedParser(t *testing.T) {
	t.Parallel()
	h := newStgHarness(t)
	good := stgNote(h.opened(stgTestMember()))
	if err := checkBody(good.Type, good.Body); err != nil {
		t.Fatal(err)
	}
	bad := stgClone(good.Body["stg_release"].(map[string]any))
	bad["extra"] = true
	if err := checkBody("note", stgNote(bad).Body); err == nil {
		t.Fatal("note vocabulary bypassed the closed STG parser")
	}
}

func stgForBatch(r map[string]any, n int) map[string]any {
	r = stgClone(r)
	id := fmt.Sprintf("stg:bifrost-platform:%d", n)
	r["batch"] = id
	if r["kind"] == "started" {
		try := StgRecord(r).Int("try")
		r["run"] = fmt.Sprintf("stgrel-%d-%d-bbbbbbbb", n, try)
		r["labels"].(map[string]any)["bifrost.io/stg-release-n"] = fmt.Sprint(n)
		r["annotations"].(map[string]any)["bifrost.io/stg-release-batch"] = id
		r["lease"].(map[string]any)["who"] = fmt.Sprintf("stgrel-bifrost-platform-%d-%d", n, try)
	}
	return r
}

func TestStgMixedBalancesAndBackoffSurviveBatchChanges(t *testing.T) {
	t.Parallel()
	m, m2 := stgTestMember(), stgTestMember2()
	h := stgReady(t, m)
	h.failed(1, "run_failed", "unchanged")
	due := h.state().Debits[stgSHA("a")].NotBefore
	h.add(stgRecord("retry", "try", 2, "not_before", due.Format(time.RFC3339Nano)))
	h.at = due
	h.add(h.snapshot(2, m))
	v := h.failed(2, "run_failed", "this_try")
	h.add(memberRecord(m, "released", v.Seq))
	h.settle(m)
	s := h.state()
	due = s.Debits[stgSHA("a")].NotBefore
	open := stgForBatch(h.opened(m, m2), 2)
	h.reject(open, "conflict") // same member's first pending clock cannot restart
	open["first_pending_at"].(map[string]any)[stgSHA("a")] = s.Members[stgMember(m)].FirstPendingAt.Format(time.RFC3339Nano)
	h.add(open)
	h.add(stgForBatch(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)), 2))
	h.add(stgForBatch(h.ci("ci-new-batch"), 2))
	h.reject(stgForBatch(h.snapshot(1, m, m2), 2), "budget")
	snap := stgForBatch(h.snapshot(1, m, m2), 2)
	snap["not_before"] = due.Format(time.RFC3339Nano)
	h.add(snap)
	h.reject(stgForBatch(h.start(1), 2), "transition")
	h.at = due
	start := h.add(stgForBatch(h.start(1), 2))
	spent := start.Record["spend"].([]any)
	if len(spent) != 2 || StgRecord(spent[0].(map[string]any)).Int("n") != 3 || StgRecord(spent[1].(map[string]any)).Int("n") != 1 {
		t.Fatal("mixed balances were reset", spent)
	}
	h.add(stgForBatch(stgRecord("bound", "try", 1, "uid", "batch2-run"), 2))
	h.add(stgForBatch(stgRecord("run_evidence", "try", 1, "uid", "batch2-run", "status", "Failed", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", h.at.Format(time.RFC3339Nano)), 2))
	h.add(stgForBatch(stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", ""), 2))
	h.add(stgForBatch(stgRecord("effect", "try", 1, "class", "unchanged", "workloads", map[string]any{"observed": true}, "at", h.at.Format(time.RFC3339Nano)), 2))
	h.reject(stgForBatch(stgRecord("retry", "try", 2, "not_before", h.at.Add(time.Hour).Format(time.RFC3339Nano)), 2), "budget")
}

func TestStgBlockUnknownBaselineAndStableDiagnosticClock(t *testing.T) {
	t.Parallel()
	h := newStgHarness(t)
	r := stgRecord("blocked", "block", StgBlockID(stgSHA("b"), "", "baseline_unknown", "v1"), "why", "baseline_unknown", "P", stgSHA("b"), "base", map[string]any{"P": ""}, "first_at", h.at.Format(time.RFC3339Nano), "evidence_version", "v1", "detail", map[string]any{"error": "unsettled"})
	first := h.add(r)
	for i := 0; i < 100; i++ {
		later := stgClone(r)
		later["first_at"] = h.at.Format(time.RFC3339Nano)
		h.reject(later, "duplicate")
		h.at = h.at.Add(time.Minute)
	}
	if !h.state().Blocks[StgRecord(r).String("block")].Opened.At.Equal(first.At) {
		t.Fatal("scan reset interval clock")
	}
	h.reject(h.opened(stgTestMember()), "transition")
	h.add(stgRecord("unblocked", "block", r["block"], "why", "evidence_changed", "ref", "observation-v2"))
	h.add(h.opened(stgTestMember()))
	d := stgRecord("diag", "try", 0, "what", "ci_waiting", "gen", 1)
	h.add(d)
	h.reject(d, "duplicate")
	if h.state().Batches[stgTestBatch].Phase != "OPEN" {
		t.Fatal("diagnostic moved phase")
	}
}

func (h *stgHarness) landed(op, root, r string) {
	ref := "refs/heads/land/prod/" + root + "/1"
	for _, record := range []map[string]any{
		stgLegacyRecord(op, "claimed", "task", root, "attempt", "a_01ARZ3NDEKTSV4RRFFQ69G5FAV", "A", r, "rule_version", 1),
		stgLegacyRecord(op, "prepared", "R", r, "M0", stgSHA("e"), "committer_date", h.at.Format(time.RFC3339Nano), "dry_run", false),
		stgLegacyRecord(op, "branch_intent", "ref", ref, "R", r), stgLegacyRecord(op, "branch_pushed", "ref", ref, "R", r),
	} {
		h.raw(map[string]any{"text": "landed fixture", "landing": record})
	}
	for _, record := range []map[string]any{
		stgLegacyRecord(op, "ci_seen", "run", "ci1", "status", "Succeeded", "R", r, "created_at", h.at.Format(time.RFC3339Nano)),
		stgLegacyRecord(op, "main_intent", "R", r, "M0", stgSHA("e")), stgLegacyRecord(op, "main_result", "outcome", "ok"), stgLegacyRecord(op, "landed", "R", r),
	} {
		h.raw(map[string]any{"text": "landed fixture", "landing": record})
	}
}
func TestStgDisposedRequiresAnAuthorizedLandingReversal(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	h.landed(m["op"].(string), stgTestRoot, stgSHA("a"))
	h.raw(map[string]any{"text": "reversed", "landing": stgLegacyRecord(m["op"].(string), "reverted", "R", stgSHA("a"), "by", stgSHA("f"))})
	ref := int64(len(h.entries))
	disposition := memberRecord(m, "disposed", ref)
	h.reject(disposition, "transition") // a human revert is not authorized
	h.landed("land:reversal:1", stgTestRoot2, stgSHA("f"))
	h.add(disposition)
	h.settle(m)
	if h.state().Members[stgMember(m)].To != "disposed" {
		t.Fatal("authorized reversal not disposed")
	}
}

func TestStgPermissionDeadlineAndTerminalGuards(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	for _, change := range []func(map[string]any){
		func(r map[string]any) { r["deadline"] = h.at.Format(time.RFC3339Nano) },
		func(r map[string]any) { r["deadline"] = h.at.Add(3 * time.Minute).Format(time.RFC3339Nano) },
		func(r map[string]any) { r["lease"].(map[string]any)["who"] = "other-holder" },
		func(r map[string]any) { r["labels"].(map[string]any)["bifrost.io/stg-release-n"] = "2" },
	} {
		r := h.start(1)
		change(r)
		h.reject(r, "conflict")
	}
	h.add(h.start(1))
	h.add(stgRecord("bound", "try", 1, "uid", "run-uid-1"))
	terminal := stgRecord("run_evidence", "try", 1, "uid", "wrong-uid", "status", "Failed", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", h.at.Format(time.RFC3339Nano))
	h.reject(terminal, "transition")
	terminal["uid"] = "run-uid-1"
	h.add(terminal)
	h.reject(stgRecord("verify", "try", 1, "outcome", "verified", "why", "complete", "evidence", "cluster/cicd/base-uid"), "conflict")
	h.add(stgRecord("effect", "try", 1, "class", "unchanged", "workloads", map[string]any{"observed": true}, "at", h.at.Format(time.RFC3339Nano)))
	h.add(stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", ""))
	h.reject(stgRecord("effect", "try", 1, "class", "other", "workloads", map[string]any{"observed": true}, "at", h.at.Format(time.RFC3339Nano)), "conflict")
}
