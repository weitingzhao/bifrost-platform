package store

import (
	"reflect"
	"testing"
	"time"
)

func stgUnblock(block StgEvent, why string, ref any) map[string]any {
	return stgRecord("unblocked", "block", block.Record.String("block"), "why", why, "ref", ref)
}

func TestStgWrongInputCannotReopenUnchangedBatch(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	h.failed(1, "wrong_input", "unchanged")
	block := h.block("wrong_input", m)
	h.add(memberRecord(m, "released", block.Seq))
	h.settle(m)
	for _, why := range []string{"rule_changed", "evidence_changed", "external_release", "granted", "inputs_changed"} {
		h.reject(stgUnblock(block, why, "same-rule"), "transition")
	}
	oldCI := h.state().Batches[stgTestBatch].CI["ci-uid"]
	h.reject(stgUnblock(block, "inputs_changed", oldCI.Seq), "transition")
	open := stgForBatch(h.opened(m), 2)
	open["first_pending_at"].(map[string]any)[m["R"].(string)] = h.state().Members[stgMember(m)].FirstPendingAt.Format(time.RFC3339Nano)
	h.at = h.state().Debits[m["R"].(string)].NotBefore
	h.reject(open, "transition")
	if h.state().Budget[StgBudgetKey{m["R"].(string), 1}] != 1 || h.state().Blocks[block.Record.String("block")].Opened.Seq != block.Seq {
		t.Fatal("refusal reset the budget or blocking clock")
	}
	// A fresh observation of the SAME P cannot resolve wrong_input.
	h.effective(m, stgSHA("b"), "effective")
	h.reject(stgUnblock(block, "inputs_changed", int64(len(h.entries))), "transition")
	// Same entry, positive control: a recorded new main head closes it.
	h.effective(m, stgSHA("f"), "effective")
	h.add(stgUnblock(block, "inputs_changed", int64(len(h.entries))))
	prefix, at := h.entries, h.at
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "old_inputs_still_refused", true: "new_inputs_send"}[changed], func(t *testing.T) {
			t.Parallel()
			f := &stgHarness{t: t, entries: append([]StgEntry(nil), prefix...), at: at}
			p := stgSHA("b")
			if changed {
				p = stgSHA("f")
			}
			f.add(stgClone(open))
			f.add(stgForBatch(stgRecord("p_selected", "P", p, "at", f.at.Format(time.RFC3339Nano)), 2))
			ci := stgForBatch(f.ci("ci-uid"), 2)
			ci["P"], ci["ci"].(map[string]any)["platform_commit"] = p, p
			f.add(ci)
			snap := stgForBatch(f.snapshot(1, m), 2)
			snap["inputs"].(map[string]any)["revision"] = p
			snap["inputs"].(map[string]any)["params_canonical"].(map[string]any)["revision"] = p
			f.add(snap)
			start := stgForBatch(f.start(1), 2)
			start["run"] = "stgrel-2-1-" + p[:8]
			if changed {
				f.add(start)
				if f.state().Budget[StgBudgetKey{m["R"].(string), 1}] != 2 {
					t.Fatal("replacement batch did not inherit its debit")
				}
			} else {
				f.reject(start, "transition")
			}
		})
	}
}

func TestStgUnblockRequiresCauseBoundProof(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"wrong_input", "ci_red", "ci_results", "ci_missing"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, stgTestMember())
			block := h.block(reason)
			h.reject(stgUnblock(block, "rule_changed", "same-rule"), "transition")
			h.reject(stgUnblock(block, "inputs_changed", "new-ci"), "transition")
			old := h.state().Batches[stgTestBatch].CI["ci-uid"]
			h.reject(stgUnblock(block, "inputs_changed", old.Seq), "transition")
			ci := h.add(h.ci("new-ci"))
			h.add(stgUnblock(block, "inputs_changed", ci.Seq))
			h.add(h.start(1))
		})
	}
	for _, reason := range []string{"baseline_unknown", "baseline_moved", "diff_unlanded", "non_linear", "no_merge_after_landing", "hard_floor"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			h := newStgHarness(t)
			h.baseline()
			block := h.block(reason)
			h.reject(stgUnblock(block, "external_release", "cluster/cicd/base-uid"), "transition")
			unrelated := stgTestEvidence(h.at, "cluster/cicd/unrelated", "external", "unrelated")
			unrelated["P"] = stgSHA("f")
			h.add(unrelated)
			h.reject(stgUnblock(block, "external_release", "cluster/cicd/unrelated"), "transition")
			h.add(stgTestEvidence(h.at, "cluster/cicd/target", "external", "manual"))
			h.add(stgUnblock(block, "external_release", "cluster/cicd/target"))
		})
	}
	t.Run("rule_needs_recorded_contract", func(t *testing.T) {
		t.Parallel()
		h := stgReady(t, stgTestMember())
		block := h.block("rule")
		h.reject(stgUnblock(block, "rule_changed", "same-rule"), "transition")
		ci := h.add(h.ci("new-ci"))
		h.reject(stgUnblock(block, "rule_changed", ci.Seq), "transition")
		h.reject(stgUnblock(block, "inputs_changed", ci.Seq), "transition")
	})
	t.Run("grant_cannot_close_a_different_interval", func(t *testing.T) {
		t.Parallel()
		m := stgTestMember()
		h := stgExhausted(t)
		block := h.block("budget_exhausted", m)
		h.ownerOutcome("q1", block, "retry_release", m)
		h.add(stgRecord("granted", "members", []any{m}, "gen", 2, "trigger_seq", block.Seq, "question_id", "q1"))
		h.add(stgUnblock(block, "granted", "q1"))
		second := h.block("budget_exhausted", m)
		h.reject(stgUnblock(second, "granted", "q1"), "transition")
		h.reject(stgUnblock(second, "evidence_changed", "revision-2"), "transition")
	})
}

func TestStgUnblockRejectsValidProofForWrongCause(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	block := h.block("wrong_input", m)
	h.add(stgTestEvidence(h.at, "cluster/cicd/target", "external", "manual"))
	h.reject(stgUnblock(block, "external_release", "cluster/cicd/target"), "transition")
	ci := h.add(h.ci("new-ci"))
	h.add(stgUnblock(block, "inputs_changed", ci.Seq))
}

func TestStgCIUnblockRequiresChangedUID(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	v := h.failed(1, "run_failed", "unchanged")
	h.add(memberRecord(m, "released", v.Seq))
	h.settle(m)
	open := stgForBatch(h.opened(m), 2)
	open["first_pending_at"].(map[string]any)[m["R"].(string)] = h.state().Members[stgMember(m)].FirstPendingAt.Format(time.RFC3339Nano)
	h.add(open)
	h.add(stgForBatch(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)), 2))
	block := h.block("ci_red")
	old := h.add(stgForBatch(h.ci("ci-uid"), 2))
	h.reject(stgUnblock(block, "inputs_changed", old.Seq), "transition")
	stale := stgForBatch(h.ci("stale-ci"), 2)
	stale["ci"].(map[string]any)["created_at"] = block.At.Add(-time.Minute).Format(time.RFC3339Nano)
	old = h.add(stale)
	h.reject(stgUnblock(block, "inputs_changed", old.Seq), "transition")
	fresh := h.add(stgForBatch(h.ci("new-ci"), 2))
	h.add(stgUnblock(block, "inputs_changed", fresh.Seq))
}

func TestStgUnblockCannotReuseOldBaseline(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	block := h.block("baseline_moved")
	id := "cluster/cicd/target"
	h.add(stgTestEvidence(h.at, id, "external", "manual"))
	h.add(stgUnblock(block, "external_release", id))
	h.reject(h.start(1), "transition")
	h.add(memberRecord(m, "released", block.Seq))
	h.settle(m)
	open := stgForBatch(h.opened(m), 2)
	open["first_pending_at"].(map[string]any)[m["R"].(string)] = h.state().Members[stgMember(m)].FirstPendingAt.Format(time.RFC3339Nano)
	h.add(open)
	h.add(stgForBatch(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)), 2))
	h.add(stgForBatch(h.ci("new-ci"), 2))
	snapshot := stgForBatch(h.snapshot(1, m), 2)
	snapshot["base"].(map[string]any)["P"] = stgSHA("b")
	snapshot["base"].(map[string]any)["evidence"] = id
	h.add(snapshot)
	h.add(stgForBatch(h.start(1), 2))
}

func TestStgEvidenceUsesBoundUID(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, id, run, source, code string }{
		{"renamed_external", "cluster/cicd/run-uid-1", "renamed-manual-run", "external", "conflict"},
		{"renamed_automatic", "cluster/cicd/run-uid-1", "renamed-manual-run", "automatic", "transition"},
		{"changed_namespace", "cluster/elsewhere/run-uid-1", "stgrel-1-1-bbbbbbbb", "automatic", "conflict"},
		{"changed_cluster_external", "elsewhere/cicd/run-uid-1", "manual", "external", "conflict"},
		{"same_name_wrong_uid_automatic", "cluster/cicd/other-uid", "stgrel-1-1-bbbbbbbb", "automatic", "transition"},
		{"same_name_independent_external", "cluster/cicd/other-uid", "stgrel-1-1-bbbbbbbb", "external", ""},
		{"independent_external", "cluster/cicd/other-uid", "manual", "external", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, stgTestMember())
			start := h.add(h.start(1))
			h.terminal(1, "Succeeded")
			r := stgTestEvidence(h.at, tc.id, tc.source, tc.run)
			if tc.code == "" {
				h.add(r)
			} else {
				h.reject(r, tc.code)
			}
			h.add(stgTestEvidence(h.at, "cluster/cicd/run-uid-1", "automatic", start.Record.String("run")))
			h.add(stgRecord("verify", "try", 1, "outcome", "verified", "why", "complete", "evidence", "cluster/cicd/run-uid-1"))
		})
	}
}

func TestStgBoundCannotRelabelExistingExternalUID(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	h.add(stgTestEvidence(h.at, "cluster/cicd/external-1", "external", "manual"))
	h.reject(stgRecord("bound", "try", 1, "uid", "external-1"), "conflict")
	h.add(stgRecord("bound", "try", 1, "uid", "local-uid"))
}

func TestStgOpenBlockStopsNewAndRecoveryPermits(t *testing.T) {
	t.Parallel()
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "started", true: "resend"}[recovery], func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, stgTestMember())
			var start StgEvent
			if recovery {
				start = h.add(h.start(1))
				h.at = start.Record.Time("deadline")
			}
			block := h.block("ci_red")
			permit := h.start(1)
			if recovery {
				permit = stgRecord("resend", "try", 1, "intent_seq", start.Seq, "lease", map[string]any{"who": "stgrel-bifrost-platform-1-1", "gen": 2}, "deadline", h.at.Add(2*time.Minute).Format(time.RFC3339Nano), "why", "not_found_unbound")
			}
			h.reject(permit, "transition")
			// Same input/entry succeeds without the blocking event. In the
			// real stream the interval must first receive a legal resolution.
			control := &stgHarness{t: t, entries: append([]StgEntry(nil), h.entries[:block.Seq-1]...), at: h.at}
			control.add(stgClone(permit))
			if !recovery {
				ci := h.add(h.ci("new-ci"))
				h.add(stgUnblock(block, "inputs_changed", ci.Seq))
				h.add(h.start(1))
			} else {
				h.add(stgRecord("unsent", "try", 1, "proof", "gate_failed_before_send"))
			}
		})
	}
	t.Run("rule_does_not_prevent_reconciliation", func(t *testing.T) {
		t.Parallel()
		m := stgTestMember()
		h := stgReady(t, m)
		h.add(h.start(1))
		h.block("rule")
		h.add(stgRecord("bound", "try", 1, "uid", "run-uid-1"))
		h.add(stgRecord("quarantined", "try", 1, "why", "object_lost", "at", h.at.Format(time.RFC3339Nano)))
		h.terminal(1, "Failed")
		v := h.add(stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", ""))
		h.add(memberRecord(m, "released", v.Seq))
		h.settle(m)
	})
}

func TestStgRuleBlockStopsFrozenAndExpiredPermits(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"started", "resend"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, stgTestMember())
			var first StgEvent
			if kind == "resend" {
				first = h.add(h.start(1))
				h.at = first.Record.Time("deadline")
			}
			block := h.block("rule")
			r := h.start(1)
			if kind == "resend" {
				r = stgRecord("resend", "try", 1, "intent_seq", first.Seq, "lease", map[string]any{"who": "stgrel-bifrost-platform-1-1", "gen": 2}, "deadline", h.at.Add(2*time.Minute).Format(time.RFC3339Nano), "why", "not_found_unbound")
			}
			h.reject(r, "transition")
			control := &stgHarness{t: t, entries: append([]StgEntry(nil), h.entries[:block.Seq-1]...), at: h.at}
			control.add(r)
		})
	}
}

func stgDeploymentCheck(at time.Time, id string) map[string]any {
	e := stgTestEvidence(at, id, "external", "manual")
	return stgRecord("deployment_checked", "evidence", id, "content", e["content"], "deployed", e["deployed"], "health", e["health"])
}

func TestStgSupersededNeedsPostTerminalDeploymentCheck(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	id := "cluster/cicd/external-1"
	h.add(stgTestEvidence(h.at, id, "external", "manual"))
	// T1's deployment observation may be overwritten by the still-running
	// automatic run at T2. Even a recheck before T2 cannot settle it.
	h.add(stgDeploymentCheck(h.at, id))
	h.terminal(1, "Succeeded")
	verify := stgRecord("verify", "try", 1, "outcome", "superseded", "why", "manual", "evidence", id)
	h.reject(verify, "conflict")
	bad := stgDeploymentCheck(h.at, "cluster/cicd/missing")
	h.reject(bad, "conflict")
	bad = stgDeploymentCheck(h.at, id)
	bad["content"].(map[string]any)["api_digest"] = stgDig("f")
	h.reject(bad, "conflict")
	bad = stgDeploymentCheck(h.at.Add(time.Minute), id)
	h.reject(bad, "invalid")
	// Re-reading this immutable evidence's content after T2 is appendable.
	check := stgDeploymentCheck(h.at, id)
	h.add(check)
	h.reject(check, "invalid")
	h.add(verify)
	if len(h.state().Evidence) != 2 || len(h.state().DeploymentChecks) != 1 {
		t.Fatal("recheck duplicated evidence instead of recording an observation")
	}
}

func TestStgSupersededRejectsLateAppendOfStaleObservation(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	h.add(h.start(1))
	id := "cluster/cicd/external-1"
	h.add(stgTestEvidence(h.at, id, "external", "manual"))
	oldAt := h.at
	h.terminal(1, "Succeeded")
	// Appending later does not make a pre-terminal read fresh.
	h.add(stgDeploymentCheck(oldAt, id))
	r := stgRecord("verify", "try", 1, "outcome", "superseded", "why", "manual", "evidence", id)
	h.reject(r, "conflict")
	h.add(stgDeploymentCheck(h.at, id))
	h.add(r)
}

func TestStgRetryFrozenFieldsAreIndependentGuards(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"inputs", "base", "set", "not_before"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			m := stgTestMember()
			h := stgReady(t, m)
			h.failed(1, "run_failed", "unchanged")
			due := h.state().Debits[m["R"].(string)].NotBefore.Add(time.Hour)
			h.add(stgRecord("retry", "try", 2, "not_before", due.Format(time.RFC3339Nano)))
			h.at = due
			good := h.snapshot(2, m)
			bad := stgClone(good)
			switch field {
			case "inputs":
				bad["inputs"].(map[string]any)["params_canonical"].(map[string]any)["extra"] = "legal-string"
			case "base":
				proof := stgTestEvidence(h.at, "cluster/cicd/other-base", "external", "other-base")
				proof["P"] = stgSHA("e")
				proof["content"].(map[string]any)["api_digest"] = stgDig("f")
				h.add(proof)
				base := bad["base"].(map[string]any)
				base["evidence"], base["api_digest"] = "cluster/cicd/other-base", stgDig("f")
			case "set":
				bad["set"] = []any{} // legal subset; only frozen equality rejects it
			case "not_before":
				bad["not_before"] = due.Add(-time.Minute).Format(time.RFC3339Nano) // still beyond inherited member backoff
			}
			h.reject(bad, "conflict")
			h.add(good)
		})
	}
}

func TestStgFailedEvidenceMustBeString(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"null", nil}, {"number", 1}, {"boolean", false}, {"object", map[string]any{"id": "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := stgReady(t, stgTestMember())
			h.add(h.start(1))
			h.terminal(1, "Failed")
			r := stgRecord("verify", "try", 1, "outcome", "failed", "why", "run_failed", "evidence", tc.value)
			h.reject(r, "invalid")
			r["evidence"] = ""
			h.add(r)
		})
	}
}

func TestStgFoldDetachesValidityAndAnswers(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := newStgHarness(t)
	h.effective(m, stgSHA("b"), "effective")
	h.ownerOutcome("q1", StgEvent{Seq: 1, Record: StgRecord{"block": "block"}}, "retry_release", stgClone(m))
	s := h.state()
	before := h.state()
	if !reflect.DeepEqual(s, before) || !s.effective(stgMember(m), stgSHA("b")) {
		t.Fatal("unmodified fold changed input semantics")
	}
	validity := h.entries[0].Event.Body["landing_main"].(map[string]any)
	answer := h.entries[1].Event.Body["landing_card_outcome"].(map[string]any)
	validity["validity"] = "unknown"
	answer["members"].([]any)[0].(map[string]any)["R"] = stgSHA("f")
	answer["choice"] = "do_not_release"
	if !s.effective(stgMember(m), stgSHA("b")) || s.Answers["q1"].Record.String("choice") != "retry_release" || s.Answers["q1"].Record.members("members")[0].R != stgSHA("a") {
		t.Fatal("caller input mutated the returned fold")
	}
	s.Validity[StgValidityKey{stgMember(m), stgSHA("b")}].Record["validity"] = "reverted"
	s.Answers["q1"].Record["members"].([]any)[0].(map[string]any)["R"] = stgSHA("2")
	if validity["validity"] != "unknown" || answer["members"].([]any)[0].(map[string]any)["R"] != stgSHA("f") || !before.effective(stgMember(m), stgSHA("b")) {
		t.Fatal("returned fold mutated its input or another fold")
	}
}
