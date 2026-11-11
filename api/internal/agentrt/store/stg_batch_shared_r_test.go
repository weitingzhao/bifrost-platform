package store

import "testing"

// A landing batch records its R for every member (landinggit), so the members
// of one batch share R. B5 opens, snapshots, starts and settles such a batch,
// and one try spends the shared R's budget once (2026-11-11: wave M-a's three
// members at e0ac7826 failed every opened check as "invalid members").
func TestStgLandingBatchMembersShareR(t *testing.T) {
	t.Parallel()
	r := stgSHA("a")
	m1 := map[string]any{"root": stgTestRoot, "op": "land:one:1", "R": r}
	m2 := map[string]any{"root": stgTestRoot2, "op": "land:two:1", "R": r}
	m3 := map[string]any{"root": "t_01ARZ3NDEKTSV4RRFFQ69G5FAX", "op": "land:three:1", "R": r}
	h := stgReady(t, m1, m2, m3)
	start := h.add(h.start(1))
	if spend := start.Record["spend"].([]any); len(spend) != 1 {
		t.Fatalf("spend = %v, want one debit for the shared R", spend)
	}
	if s := h.state(); s.Budget[StgBudgetKey{r, 1}] != 1 || s.Debits[r].Total != 1 {
		t.Fatalf("budget %d, debits %d: a try spends a shared R once", s.Budget[StgBudgetKey{r, 1}], s.Debits[r].Total)
	}
	h.terminal(1, "Succeeded")
	h.add(stgTestEvidence(h.at, "cluster/cicd/run-uid-1", "automatic", start.Record.String("run")))
	verify := h.add(stgRecord("verify", "try", 1, "outcome", "verified", "why", "complete", "evidence", "cluster/cicd/run-uid-1"))
	for _, m := range []map[string]any{m1, m2, m3} {
		h.effective(m, stgSHA("b"), "effective")
		h.add(memberRecord(m, "delivered", verify.Seq))
	}
	h.settle(m1, m2, m3)
	if s := h.state(); s.Batches[stgTestBatch].Phase != "CLOSED" {
		t.Fatalf("phase %s, want CLOSED", s.Batches[stgTestBatch].Phase)
	}
}

// The same member twice is still refused; only distinct members may share R.
func TestStgOpenedRefusesTheSameMemberTwice(t *testing.T) {
	t.Parallel()
	h := newStgHarness(t)
	h.baseline()
	m := stgTestMember()
	h.reject(h.opened(m, m), "")
}

// Both shapes open in one batch: members of a landing batch sharing R, and a
// member landed alone with an R of its own; one try spends each R once.
func TestStgBatchMixesSharedAndDistinctR(t *testing.T) {
	t.Parallel()
	shared, own := stgSHA("a"), stgSHA("2")
	m1 := map[string]any{"root": stgTestRoot, "op": "land:one:1", "R": shared}
	m2 := map[string]any{"root": stgTestRoot2, "op": "land:two:1", "R": shared}
	m3 := map[string]any{"root": "t_01ARZ3NDEKTSV4RRFFQ69G5FAX", "op": "land:three:1", "R": own}
	h := stgReady(t, m1, m2, m3)
	start := h.add(h.start(1))
	if spend := start.Record["spend"].([]any); len(spend) != 2 {
		t.Fatalf("spend = %v, want one debit per distinct R", spend)
	}
	s := h.state()
	if s.Budget[StgBudgetKey{shared, 1}] != 1 || s.Budget[StgBudgetKey{own, 1}] != 1 {
		t.Fatalf("budget shared %d own %d, want 1 and 1", s.Budget[StgBudgetKey{shared, 1}], s.Budget[StgBudgetKey{own, 1}])
	}
}
