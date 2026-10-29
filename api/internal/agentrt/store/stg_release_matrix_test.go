package store

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func (h *stgHarness) failed(n int64, why, effect string) StgEvent {
	h.add(h.start(n))
	status := "Succeeded"
	if why == "run_failed" {
		status = "Failed"
	}
	h.terminal(n, status)
	v := h.add(stgRecord("verify", "try", n, "outcome", "failed", "why", why, "evidence", ""))
	h.add(stgRecord("effect", "try", n, "class", effect, "workloads", map[string]any{"observed": true}, "at", h.at.Format(time.RFC3339Nano)))
	return v
}
func stgExhausted(t *testing.T) *stgHarness {
	t.Helper()
	m := stgTestMember()
	h := stgReady(t, m)
	var v StgEvent
	for n := int64(1); n <= 3; n++ {
		if n > 1 {
			due := h.state().Debits[stgSHA("a")].NotBefore
			h.add(stgRecord("retry", "try", n, "not_before", due.Format(time.RFC3339Nano)))
			h.at = due
			h.add(h.snapshot(n, m))
		}
		v = h.failed(n, "run_failed", "unchanged")
	}
	h.add(memberRecord(m, "exhausted", v.Seq))
	h.settle(m)
	return h
}

// The STG card family is B5-g. These replay fixtures describe its required
// recorded answer, not a mocked Owner RPC and not a new card-writing entry.
func (h *stgHarness) ownerOutcome(q string, trigger StgEvent, choice string, ms ...map[string]any) {
	kind := "stg_failed"
	if choice == "accept_unreconciled" {
		kind = "stg_blocked"
	}
	r := map[string]any{"family": "stg", "kind": kind, "question_id": q, "trigger_seq": trigger.Seq, "outcome": "applied", "choice": choice, "channel": "console", "by": "owner"}
	if id := trigger.Record.String("block"); id != "" {
		r["block"] = id
	}
	if id := trigger.Record.String("batch"); id != "" {
		r["batch"] = id
	}
	if len(ms) > 0 {
		a := []any{}
		for _, m := range ms {
			a = append(a, m)
		}
		r["members"] = a
	}
	h.raw(map[string]any{"text": "applied STG card fixture", "landing_card_outcome": r})
}

func TestStgGrantWithholdingAndBlockIntervals(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgExhausted(t)
	block := h.block("budget_exhausted", m)
	grant := stgRecord("granted", "members", []any{m}, "gen", 2, "trigger_seq", block.Seq, "question_id", "q1")
	h.reject(grant, "permission")
	h.ownerOutcome("q1", block, "retry_release", m)
	bad := stgClone(grant)
	bad["gen"] = 3
	h.reject(bad, "budget")
	h.add(grant)
	h.reject(grant, "duplicate")
	h.add(stgRecord("unblocked", "block", block.Record.String("block"), "why", "granted", "ref", "q1"))
	state := h.state()
	if state.Budget[StgBudgetKey{stgSHA("a"), 1}] != 3 || state.Budget[StgBudgetKey{stgSHA("a"), 2}] != 0 || state.Debits[stgSHA("a")].NotBefore.IsZero() {
		t.Fatal("grant reset history")
	}
	// Reopen the same interval only after it was closed; preserve both times.
	second := h.block("budget_exhausted", m)
	h.reject(map[string]any(second.Record), "duplicate")
	if len(h.state().BlockHistory) != 2 {
		t.Fatal("lost previous blocking interval")
	}
	h.reject(stgRecord("unblocked", "block", second.Record.String("block"), "why", "evidence_changed", "ref", "revision-2"), "transition")
	// Opening another interval cannot reuse the first interval's grant.
	h.reject(stgUnblock(second, "granted", "q1"), "transition")
	// The new-generation positive control uses the legally resolved prefix.
	h.entries = h.entries[:second.Seq-1]
	open := h.opened(m)
	open["batch"] = "stg:bifrost-platform:2"
	open["first_pending_at"] = map[string]any{stgSHA("a"): state.Members[stgMember(m)].FirstPendingAt.Format(time.RFC3339Nano)}
	h.add(open)
	if h.state().Members[stgMember(m)].Gen != 2 {
		t.Fatal("new generation not inherited")
	}

	w := stgExhausted(t)
	b := w.block("budget_exhausted", m)
	w.ownerOutcome("q1", b, "do_not_release", m)
	w.add(stgRecord("withheld", "members", []any{m}, "why", "owner", "trigger_seq", b.Seq, "question_id", "q1"))
	if len(w.state().Withheld) != 1 {
		t.Fatal("owner withholding lost")
	}
	w.add(stgRecord("unblocked", "block", b.Record.String("block"), "why", "granted", "ref", "q1"))
	open = w.opened(m)
	open["batch"] = "stg:bifrost-platform:2"
	w.reject(open, "budget")

	f := stgReady(t, m)
	floor := f.block("hard_floor", m)
	f.add(stgRecord("withheld", "grant", "floor-1", "members", []any{m}, "why", "hard_floor", "trigger_seq", floor.Seq))
	f.reject(f.start(1), "budget")
}

func TestStgQuarantineOwnerExitAndNoRefund(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	h.add(h.start(1))
	q := h.add(stgRecord("quarantined", "try", 1, "why", "unknown_execution", "at", h.at.Format(time.RFC3339Nano)))
	close := stgRecord("quarantine_closed", "try", 1, "question_id", "q-risk", "by", "owner")
	h.reject(close, "permission")
	h.ownerOutcome("q-risk", q, "accept_unreconciled")
	closed := h.add(close)
	h.add(memberRecord(m, "released", closed.Seq))
	h.settle(m)
	s := h.state()
	if s.Batches[stgTestBatch].Tries[1].RunEvidence.Seq != 0 || s.Budget[StgBudgetKey{stgSHA("a"), 1}] != 1 {
		t.Fatal("risk acceptance claimed reconciliation or refunded")
	}
}

func TestStgCIIdentityAndFrozenInputRefusals(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := newStgHarness(t)
	h.baseline()
	h.add(h.opened(m))
	h.reject(h.snapshot(1, m), "transition")
	h.reject(h.ci("ci-1"), "conflict")
	h.add(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)))
	h.add(h.ci("ci-1"))
	h.reject(h.ci("ci-1"), "duplicate")
	bad := h.ci("ci-2")
	bad["ci"].(map[string]any)["ui_commit"] = stgSHA("f")
	h.reject(bad, "conflict")
	h.add(h.ci("ci-2"))
	bad = h.snapshot(1, m)
	bad["set"] = []any{stgTestMember2()}
	h.reject(bad, "budget")
	bad = h.snapshot(1, m)
	bad["base"].(map[string]any)["api_digest"] = stgDig("f")
	h.reject(bad, "transition")
	h.add(h.snapshot(1, m))
	h.add(h.ci("ci-3"))
	h.add(stgRecord("diag", "try", 1, "what", "lease_lost", "gen", 1))
	if h.state().Batches[stgTestBatch].Phase != "SNAP" {
		t.Fatal("diagnostic changed phase")
	}
	h.add(h.start(1))
	h.reject(h.ci("ci-4"), "transition")
}

func TestStgExternalIdentityLateCoversAndWrongInput(t *testing.T) {
	t.Parallel()
	m, m2 := stgTestMember(), stgTestMember2()
	h := stgReady(t, m)
	start := h.add(h.start(1))
	h.terminal(1, "Succeeded")
	local := stgTestEvidence(h.at, "cluster/cicd/run-uid-1", "external", start.Record.String("run"))
	h.reject(local, "conflict")
	external := stgTestEvidence(h.at, "cluster/cicd/external-1", "external", "manual-run")
	h.add(external)
	h.reject(external, "duplicate")
	cover := stgRecord("covers")
	for k, v := range m {
		cover[k] = v
	}
	h.reject(cover, "transition")
	h.effective(m, stgSHA("b"), "effective")
	h.add(cover)
	h.reject(cover, "duplicate")
	// Identical digests from another UID are distinct evidence, and the same
	// evidence can later cover a member first discovered after it was saved.
	external = stgClone(external)
	external["evidence"] = "cluster/cicd/external-2"
	external["run"] = "manual-run-2"
	h.add(external)
	h.effective(m2, stgSHA("b"), "effective")
	for k, v := range m2 {
		cover[k] = v
	}
	h.add(cover)
	if len(h.state().Evidence) != 3 || len(h.state().Covers) != 2 {
		t.Fatal("identity or late coverage lost")
	}
	wrong := stgReady(t, m)
	wrongVerify := wrong.failed(1, "wrong_input", "unchanged")
	wrong.reject(stgRecord("retry", "try", 2, "not_before", wrong.at.Add(time.Hour).Format(time.RFC3339Nano)), "transition")
	wrong.reject(memberRecord(m, "released", wrongVerify.Seq), "transition")
	b := wrong.block("wrong_input", m)
	wrong.add(memberRecord(m, "released", b.Seq))
	wrong.settle(m)
	open := wrong.opened(m)
	open["batch"] = "stg:bifrost-platform:2"
	wrong.reject(open, "transition")
}

func TestStgRestartAtEveryBoundary(t *testing.T) {
	t.Parallel()
	h := stgExhausted(t)
	full := h.state()
	// Includes OPEN, P selected, accepted CI, SNAP, STARTED, bound, raw
	// terminal evidence, verify, effect, retry, each member and settlement.
	for cut := 0; cut <= len(h.entries); cut++ {
		prefix := append([]StgEntry(nil), h.entries[:cut]...)
		replayed, err := StgFold(prefix)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range h.entries[cut:] {
			r, err := ParseStgRelease(entry.Event.Body["stg_release"])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := replayed.apply(StgEvent{entry.Seq, entry.At, r}); err != nil {
				t.Fatal(err)
			}
			replayed.LastSeq = entry.Seq
		}
		if !reflect.DeepEqual(full, replayed) {
			t.Fatalf("restart %d changed identities, budgets or times", cut)
		}
	}
}

func TestStgEveryEventKindHasSuccessAndDuplicateControl(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgExhausted(t)
	b := h.block("budget_exhausted", m)
	h.ownerOutcome("q1", b, "retry_release", m)
	h.add(stgRecord("granted", "members", []any{m}, "gen", 2, "trigger_seq", b.Seq, "question_id", "q1"))
	h.add(stgRecord("unblocked", "block", b.Record.String("block"), "why", "granted", "ref", "q1"))
	samples := slices.Clone(h.samples)
	h = stgReady(t, m)
	h.add(stgRecord("diag", "try", 1, "what", "ci_waiting", "gen", 1))
	start := h.add(h.start(1))
	h.at = h.at.Add(3 * time.Minute)
	h.add(stgRecord("resend", "try", 1, "intent_seq", start.Seq, "lease", map[string]any{"who": "stgrel-bifrost-platform-1-1", "gen": 2}, "deadline", h.at.Add(2*time.Minute).Format(time.RFC3339Nano), "why", "not_found_unbound"))
	h.at = h.at.Add(3 * time.Minute)
	h.add(stgRecord("unsent", "try", 1, "proof", "deadline_passed_not_found"))
	samples = append(samples, h.samples...)
	h = stgReady(t, m)
	h.add(h.start(1))
	q := h.add(stgRecord("quarantined", "try", 1, "why", "collision", "at", h.at.Format(time.RFC3339Nano)))
	h.ownerOutcome("q1", q, "accept_unreconciled")
	h.add(stgRecord("quarantine_closed", "try", 1, "question_id", "q1", "by", "owner"))
	samples = append(samples, h.samples...)
	h = stgReady(t, m)
	floor := h.block("hard_floor", m)
	h.add(stgRecord("withheld", "grant", "floor1", "members", []any{m}, "why", "hard_floor", "trigger_seq", floor.Seq))
	samples = append(samples, h.samples...)
	h = stgReady(t, m)
	h.add(stgTestEvidence(h.at, "cluster/cicd/external-1", "external", "manual"))
	h.add(stgDeploymentCheck(h.at, "cluster/cicd/external-1"))
	h.effective(m, stgSHA("b"), "effective")
	c := stgRecord("covers")
	for k, v := range m {
		c[k] = v
	}
	h.add(c)
	samples = append(samples, h.samples...)
	seen := map[string]bool{}
	for _, sample := range samples {
		kind := sample.record["kind"].(string)
		if seen[kind] {
			continue
		}
		seen[kind] = true
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			fixture := &stgHarness{t: t, entries: slices.Clone(sample.prefix), at: sample.at}
			fixture.add(sample.record)
			bad := stgClone(sample.record)
			if kind == "bound" {
				bad["uid"] = "conflicting-uid"
			}
			fixture.reject(bad, "")
		})
	}
	for k := range stgSpecs {
		if !seen[k] {
			t.Fatal("no legal edge and refusal control for", k)
		}
	}
}

func stgLegacyRecord(op, kind string, kv ...any) map[string]any {
	r := map[string]any{"v": 1, "op": op, "kind": kind}
	for i := 0; i < len(kv); i += 2 {
		r[kv[i].(string)] = kv[i+1]
	}
	return r
}
func TestStgMixedLegacyReplay(t *testing.T) {
	t.Parallel()
	h := newStgHarness(t)
	h.raw(map[string]any{"text": "enabled", "landing": stgLegacyRecord("enabled", "enabled", "repo", "bifrost-platform", "rule_version", 1)})
	for i, outcome := range []string{"ok", "failed"} {
		op := fmt.Sprintf("stg:old:%d", i)
		for _, r := range []map[string]any{
			stgLegacyRecord(op, "stg_pending", "R", []any{stgSHA("a")}),
			stgLegacyRecord(op, "stg_snapshot", "P", stgSHA("b"), "U", stgSHA("c"), "try", 1, "not_before", h.at.Format(time.RFC3339Nano)),
			stgLegacyRecord(op, "stg_started", "run", op, "try", 1), stgLegacyRecord(op, "stg_result", "run", op, "outcome", outcome),
		} {
			h.raw(map[string]any{"text": "legacy", "landing": r})
		}
		if i == 0 {
			h.raw(map[string]any{"text": "legacy", "landing": stgLegacyRecord(op, "stg_verified", "P", stgSHA("b"), "U", stgSHA("c"), "digests", map[string]any{"platform-api": stgDig("a"), "platform-workers": stgDig("a"), "platform-console": stgDig("c")})})
		} else {
			h.raw(map[string]any{"text": "legacy", "landing": stgLegacyRecord(op, "stg_failed", "why", "ci_failed")})
		}
		if !h.state().Legacy.Operations[op].Terminal() {
			t.Fatal("historical Terminal changed")
		}
		h.raw(map[string]any{"text": "ordinary note"})
	}
	h.baseline()
	h.add(h.opened(stgTestMember()))
	h.raw(map[string]any{"text": "ordinary landing", "landing": map[string]any{"v": "1.1", "kind": "mode", "op": "mode:live", "repo": "bifrost-platform"}})
	s := h.state()
	if len(s.Batches) != 1 || len(s.Legacy.Operations) != 2 || s.Legacy.LiveAt.IsZero() || s.Legacy.EnabledAt.IsZero() {
		t.Fatal("mixed replay lost a protocol")
	}
	bad := slices.Clone(h.entries)
	broken := stgClone(bad[2].Event.Body["landing"].(map[string]any))
	broken["U"] = strings.ToUpper(stgSHA("c"))
	bad[2].Event.Body = map[string]any{"landing": broken}
	if _, err := StgFold(bad); err == nil {
		t.Fatal("legacy decoder silently accepted malformed history")
	}
}
