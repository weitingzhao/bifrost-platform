package store

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Review 684 regressions exercise the production Append core and replay.
// Keep these fixtures runnable against ab8dc25a to reproduce all four findings.
func review684WantAllowed(t *testing.T, h *stgHarness, r map[string]any) {
	t.Helper()
	h.add(r)
}

func review684OpenNext(h *stgHarness, m map[string]any) {
	op := stgForBatch(h.opened(m), 2)
	op["first_pending_at"].(map[string]any)[m["R"].(string)] = h.state().Members[stgMember(m)].FirstPendingAt.Format(time.RFC3339Nano)
	h.add(op)
	h.add(stgForBatch(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)), 2))
}

func TestReview684ClosedBatchCIProof(t *testing.T) {
	t.Parallel()
	for _, cause := range []string{"wrong_input", "ci_red", "ci_results", "ci_missing"} {
		t.Run(cause, func(t *testing.T) {
			t.Parallel()
			m := stgTestMember()
			h := newStgHarness(t)
			if cause == "wrong_input" {
				h = stgReady(t, m)
				h.failed(1, "wrong_input", "unchanged")
			} else {
				h.baseline()
				h.add(h.opened(m))
				h.add(stgRecord("p_selected", "P", stgSHA("b"), "at", h.at.Format(time.RFC3339Nano)))
				if cause == "ci_missing" {
					h.at = h.at.Add(31 * time.Minute)
				}
			}
			block := h.block(cause, m)
			h.add(memberRecord(m, "released", block.Seq))
			h.settle(m)
			h.reject(h.ci("fresh-success"), "transition") // the closed batch stays closed
			// Section 9.3 closes a blocked batch. A fresh, successful same-P
			// CI must be recordable before that interval can be resolved.
			proof := h.add(review684CIObservation(h, block, "fresh-success"))
			h.add(stgUnblock(block, "inputs_changed", proof.Seq))
			review684OpenNext(h, m)
			h.add(review684Accept(proof, 2))
			if due := h.state().Debits[m["R"].(string)].NotBefore; h.at.Before(due) {
				h.at = due
			}
			h.add(stgForBatch(h.snapshot(1, m), 2))
			h.add(stgForBatch(h.start(1), 2))
		})
	}
}

func TestReview684ChangedUICannotSupplyUnlock(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	block := h.block("ci_red")
	ci := review684CIObservation(h, block, "green-with-new-ui")
	ci["ci"].(map[string]any)["ui_commit"] = stgSHA("f")
	h.reject(review684Accept(StgEvent{Record: StgRecord(ci)}, 1), "conflict")
	h.add(memberRecord(stgTestMember(), "released", block.Seq))
	h.settle(stgTestMember())
	proof := h.add(ci)
	if h.state().Batches[stgTestBatch].Tries[1].Snapshot.Record.object("inputs").String("uiRevision") != stgSHA("c") || len(h.state().Batches[stgTestBatch].CI) != 1 {
		t.Fatal("block observation changed executable inputs")
	}
	h.add(stgUnblock(block, "inputs_changed", proof.Seq))
	review684OpenNext(h, stgTestMember())
	h.add(review684Accept(proof, 2))
	snap := stgForBatch(h.snapshot(1, stgTestMember()), 2)
	review684Input(snap, "uiRevision", stgSHA("f"))
	h.add(snap)
	h.add(stgForBatch(h.start(1), 2))
}

func TestReview684CIResolutionHistoryConflicts(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	b1 := h.block("ci_red")
	c1 := h.add(h.ci("success-1"))
	h.add(stgUnblock(b1, "inputs_changed", c1.Seq))
	b2 := h.block("ci_red")
	ci2 := h.ci("success-2")
	ci2["ci"].(map[string]any)["infra_commit"] = stgSHA("f")
	c2 := h.add(ci2)
	h.add(stgUnblock(b2, "inputs_changed", c2.Seq))
	h.add(memberRecord(m, "released", b2.Seq))
	h.settle(m)
	review684OpenNext(h, m)
	// Both UIDs are present, so only the incompatible historical infra
	// requirements cause the refusal (D from interval 1, F from interval 2).
	h.add(stgForBatch(map[string]any(c1.Record), 2))
	h.add(stgForBatch(map[string]any(c2.Record), 2))
	snap := stgForBatch(h.snapshot(1, m), 2)
	snap["inputs"].(map[string]any)["infraRevision"] = stgSHA("f")
	snap["inputs"].(map[string]any)["params_canonical"].(map[string]any)["infraRevision"] = stgSHA("f")
	h.add(snap)
	review684WantAllowed(t, h, stgForBatch(h.start(1), 2))
}

func TestReview684BaselineResolutionHistoryConflicts(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	var last StgEvent
	for n := 1; n <= 2; n++ {
		last = h.block("baseline_moved")
		id := fmt.Sprintf("cluster/cicd/manual-%d", n)
		h.add(stgTestEvidence(h.at, id, "external", fmt.Sprintf("manual-%d", n)))
		h.add(stgUnblock(last, "external_release", id))
	}
	h.add(memberRecord(m, "released", last.Seq))
	h.settle(m)
	review684OpenNext(h, m)
	h.add(stgForBatch(h.ci("success-2"), 2))
	snap := stgForBatch(h.snapshot(1, m), 2)
	snap["base"].(map[string]any)["P"] = stgSHA("b")
	snap["base"].(map[string]any)["evidence"] = "cluster/cicd/manual-2"
	h.add(snap)
	review684WantAllowed(t, h, stgForBatch(h.start(1), 2))
}

func TestReview684BoundUIDCannotBelongToTwoIntents(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	h.failed(1, "run_failed", "unchanged")
	due := h.state().Debits[stgSHA("a")].NotBefore
	h.add(stgRecord("retry", "try", 2, "not_before", due.Format(time.RFC3339Nano)))
	h.at = due
	h.add(h.snapshot(2, m))
	h.add(h.start(2))
	h.reject(stgRecord("bound", "try", 2, "uid", "run-uid-1"), "conflict")
	h.add(stgRecord("bound", "try", 2, "uid", "run-uid-2"))
	h.terminal(2, "Succeeded")
	h.add(stgTestEvidence(h.at, "cluster/cicd/run-uid-2", "automatic", "stgrel-1-2-bbbbbbbb"))
}

func TestReview684CIStartedBeforeBlockCanFinishAfterBlock(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	block := h.block("ci_red")
	ci := h.ci("newly-accepted-success")
	ci["ci"].(map[string]any)["created_at"] = block.At.Add(-time.Second).Format(time.RFC3339Nano)
	proof := h.add(ci)
	review684WantAllowed(t, h, stgUnblock(block, "inputs_changed", proof.Seq))
}

func TestReview684BoundReplayRemainsIdempotent(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	start := h.add(h.start(1))
	h.terminal(1, "Succeeded")
	bound := h.state().Batches[stgTestBatch].Tries[1].Bound
	h.add(stgTestEvidence(h.at, "cluster/cicd/run-uid-1", "automatic", start.Record.String("run")))
	v := h.add(stgRecord("verify", "try", 1, "outcome", "verified", "why", "complete", "evidence", "cluster/cicd/run-uid-1"))
	h.effective(m, stgSHA("b"), "effective")
	h.add(memberRecord(m, "delivered", v.Seq))
	h.settle(m)
	_, dup, err := h.check(stgNote(stgRecord("bound", "try", 1, "uid", "run-uid-1")))
	if err != nil || dup != bound.Seq {
		t.Fatalf("same-intent replay: seq=%d err=%v", dup, err)
	}
	h.reject(stgRecord("bound", "try", 1, "uid", "other"), "conflict")
}

// Independent interval evidence is not an executable batch CI acceptance.
func review684CIObservation(h *stgHarness, block StgEvent, uid string) map[string]any {
	return map[string]any{"v": "1", "kind": "ci_observed", "scope": "block", "block": block.Record.String("block"), "interval_seq": block.Seq, "P": block.Record.String("P"), "ci": h.ci(uid)["ci"], "status": "Succeeded"}
}
func review684Accept(proof StgEvent, batch int) map[string]any {
	return stgForBatch(stgRecord("ci_accepted", "P", proof.Record.String("P"), "ci", stgClone(map[string]any(proof.Record.object("ci")))), batch)
}
func review684Input(snap map[string]any, field, value string) {
	inputs := snap["inputs"].(map[string]any)
	inputs[field] = value
	inputs["params_canonical"].(map[string]any)[field] = value
}

func review684Fork(t *testing.T, h *stgHarness) *stgHarness {
	t.Helper()
	return &stgHarness{t: t, entries: append([]StgEntry(nil), h.entries...), at: h.at}
}

func review684Refuse(t *testing.T, h *stgHarness, r map[string]any, reason string) {
	t.Helper()
	_, _, err := h.check(stgNote(r))
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("%s: error=%v, want %q", r["kind"], err, reason)
	}
}

func TestReview684LatestCIProofReplacesHistory(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"uid", "infra", "ui"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			m := stgTestMember()
			h := stgReady(t, m)
			old := h.state().Batches[stgTestBatch].CI["ci-uid"]
			first := h.block("ci_red")
			c1 := h.add(review684CIObservation(h, first, "success-1"))
			h.add(stgUnblock(first, "inputs_changed", c1.Seq))
			second := h.block("ci_red")
			ci := review684CIObservation(h, second, "success-2")
			if change == "infra" {
				ci["ci"].(map[string]any)["infra_commit"] = stgSHA("f")
			}
			if change == "ui" {
				ci["ci"].(map[string]any)["ui_commit"] = stgSHA("f")
			}
			c2 := h.add(ci)
			h.add(stgUnblock(second, "inputs_changed", c2.Seq))
			h.add(memberRecord(m, "released", second.Seq))
			h.settle(m)
			for _, tc := range []struct {
				name    string
				proof   StgEvent
				allowed bool
			}{{"original_rejected", old, false}, {"old_resolution", c1, false}, {"latest", c2, true}} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					f := review684Fork(t, h)
					review684OpenNext(f, m)
					f.add(review684Accept(tc.proof, 2))
					snap := stgForBatch(f.snapshot(1, m), 2)
					review684Input(snap, "uiRevision", tc.proof.Record.object("ci").String("ui_commit"))
					review684Input(snap, "infraRevision", tc.proof.Record.object("ci").String("infra_commit"))
					f.add(snap)
					start := stgForBatch(f.start(1), 2)
					if tc.allowed {
						f.add(start)
					} else {
						review684Refuse(t, f, start, "permit reused inputs")
					}
					if len(f.state().BlockHistory) != 2 {
						t.Fatal("replacement deleted audit history")
					}
				})
			}
		})
	}
}

func review684Base(e StgEvent) map[string]any {
	return map[string]any{"P": e.Record.String("P"), "evidence": e.Record.String("evidence"), "api_digest": e.Record.object("content").String("api_digest"), "console_digest": e.Record.object("content").String("console_digest")}
}

func TestReview684InputAndBaselineResolutionsAreIndependent(t *testing.T) {
	t.Parallel()
	for _, lastDimension := range []string{"inputs", "base"} {
		t.Run(lastDimension, func(t *testing.T) {
			t.Parallel()
			m := stgTestMember()
			h := stgReady(t, m)
			originalCI := h.state().Batches[stgTestBatch].CI["ci-uid"]
			originalBase := h.state().Evidence["cluster/cicd/base-uid"]
			b1 := h.block("baseline_moved")
			e1 := h.add(stgTestEvidence(h.at, "cluster/cicd/manual-1", "external", "manual-1"))
			h.add(stgUnblock(b1, "external_release", e1.Record.String("evidence")))
			var c2, e2, last StgEvent
			resolveCI := func() {
				last = h.block("ci_red")
				ci := review684CIObservation(h, last, "success-2")
				ci["ci"].(map[string]any)["infra_commit"] = stgSHA("f")
				c2 = h.add(ci)
				h.add(stgUnblock(last, "inputs_changed", c2.Seq))
			}
			resolveBase := func() {
				block := stgClone(map[string]any(b1.Record))
				block["base"], block["first_at"] = review684Base(e1), h.at.Format(time.RFC3339Nano)
				block["block"] = StgBlockID(stgSHA("b"), stgSHA("b"), "baseline_moved", "v1")
				last = h.add(block)
				ev := stgTestEvidence(h.at, "cluster/cicd/manual-2", "external", "manual-2")
				ev["content"].(map[string]any)["api_digest"] = stgDig("f")
				e2 = h.add(ev)
				h.add(stgUnblock(last, "external_release", e2.Record.String("evidence")))
			}
			if lastDimension == "inputs" {
				resolveBase()
				resolveCI()
			} else {
				resolveCI()
				resolveBase()
			}
			h.add(memberRecord(m, "released", last.Seq))
			h.settle(m)
			for _, tc := range []struct {
				name     string
				ci, base StgEvent
				reason   string
			}{
				{"original_base", c2, originalBase, "permit reused the baseline"},
				{"old_resolution_base", c2, e1, "permit reused the baseline"},
				{"old_inputs", originalCI, e2, "permit reused inputs"},
				{"latest_both", c2, e2, ""},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					f := review684Fork(t, h)
					review684OpenNext(f, m)
					f.add(review684Accept(tc.ci, 2))
					snap := stgForBatch(f.snapshot(1, m), 2)
					review684Input(snap, "infraRevision", tc.ci.Record.object("ci").String("infra_commit"))
					snap["base"] = review684Base(tc.base)
					f.add(snap)
					start := stgForBatch(f.start(1), 2)
					if tc.reason == "" {
						f.add(start)
					} else {
						review684Refuse(t, f, start, tc.reason)
					}
					if len(f.state().BlockHistory) != 3 {
						t.Fatal("lost audit history")
					}
				})
			}
		})
	}
}

func TestReview684BoundUIDSurvivesBatchAndQuarantine(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"cross_batch", "recovery_after_quarantine", "prior_quarantine_closed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			m := stgTestMember()
			h := stgReady(t, m)
			var decision StgEvent
			if mode == "prior_quarantine_closed" {
				h.add(h.start(1))
				h.add(stgRecord("bound", "try", 1, "uid", "run-uid-1"))
				q := h.add(stgRecord("quarantined", "try", 1, "why", "object_lost", "at", h.at.Format(time.RFC3339Nano)))
				h.ownerOutcome("q-risk", q, "accept_unreconciled")
				decision = h.add(stgRecord("quarantine_closed", "try", 1, "question_id", "q-risk", "by", "owner"))
			} else {
				decision = h.failed(1, "run_failed", "unchanged")
			}
			h.add(memberRecord(m, "released", decision.Seq))
			h.settle(m)
			review684OpenNext(h, m)
			h.add(stgForBatch(h.ci("ci-next"), 2))
			if due := h.state().Debits[m["R"].(string)].NotBefore; h.at.Before(due) {
				h.at = due
			}
			h.add(stgForBatch(h.snapshot(1, m), 2))
			start := h.add(stgForBatch(h.start(1), 2))
			if mode == "recovery_after_quarantine" {
				h.add(stgForBatch(stgRecord("quarantined", "try", 1, "why", "unknown_execution", "at", h.at.Format(time.RFC3339Nano)), 2))
			}
			review684Refuse(t, h, stgForBatch(stgRecord("bound", "try", 1, "uid", "run-uid-1"), 2), "run UID is already bound to another intent")
			h.add(stgForBatch(stgRecord("bound", "try", 1, "uid", "run-next"), 2))
			h.add(stgForBatch(stgRecord("run_evidence", "try", 1, "uid", "run-next", "status", "Succeeded", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", h.at.Format(time.RFC3339Nano)), 2))
			h.add(stgForBatch(stgTestEvidence(h.at, "cluster/cicd/run-next", "automatic", start.Record.String("run")), 2))
		})
	}
}

func TestReview684CIObservationBoundaries(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	blockRecord := stgRecord("blocked", "block", StgBlockID(stgSHA("b"), stgSHA("e"), "ci_red", "v1"), "why", "ci_red", "P", stgSHA("b"), "base", stgTestBase(), "first_at", h.at.Format(time.RFC3339Nano), "evidence_version", "v1", "detail", map[string]any{"ci": map[string]any{"uid": "failed-selection", "created_at": h.at.Add(-2 * time.Second).Format(time.RFC3339Nano), "read_at": h.at.Add(-time.Second).Format(time.RFC3339Nano)}})
	// A failed CI was never accepted into the batch. Persist its identity as
	// part of the blocking observation, without changing the legacy schema.
	block := h.add(blockRecord)
	good := review684CIObservation(h, block, "green")
	good["ci"].(map[string]any)["created_at"] = block.At.Add(-time.Second).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name, code string
		change     func(map[string]any)
	}{
		{"old_accepted_uid", "transition", func(r map[string]any) { r["ci"].(map[string]any)["uid"] = "ci-uid" }},
		{"rejected_uid", "transition", func(r map[string]any) { r["ci"].(map[string]any)["uid"] = "failed-selection" }},
		{"old_read", "transition", func(r map[string]any) { r["ci"].(map[string]any)["read_at"] = block.At.Format(time.RFC3339Nano) }},
		{"older_selection", "transition", func(r map[string]any) {
			r["ci"].(map[string]any)["created_at"] = block.At.Add(-3 * time.Second).Format(time.RFC3339Nano)
		}},
		{"future_read", "transition", func(r map[string]any) {
			r["ci"].(map[string]any)["read_at"] = h.at.Add(time.Minute).Format(time.RFC3339Nano)
		}},
		{"wrong_interval", "transition", func(r map[string]any) { r["interval_seq"] = block.Seq - 1 }},
		{"wrong_P", "transition", func(r map[string]any) { r["P"] = stgSHA("f") }},
		{"wrong_result_P", "transition", func(r map[string]any) { r["ci"].(map[string]any)["platform_commit"] = stgSHA("f") }},
		{"failed", "invalid", func(r map[string]any) { r["status"] = "Failed" }},
		{"missing_status", "invalid", func(r map[string]any) { delete(r, "status") }},
		{"extra_field", "invalid", func(r map[string]any) { r["batch"] = stgTestBatch }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := review684Fork(t, h)
			bad := stgClone(good)
			tc.change(bad)
			f.reject(bad, tc.code)
			proof := f.add(stgClone(good))
			f.add(stgUnblock(block, "inputs_changed", proof.Seq))
		})
	}
	t.Run("writer", func(t *testing.T) {
		t.Parallel()
		for _, change := range []func(*Event){func(e *Event) { e.Actor = "worker" }, func(e *Event) { e.Stream = "landing:other" }, func(e *Event) { e.TaskID = stgTestRoot }, func(e *Event) { e.Type = "result" }, func(e *Event) { e.Body["landing_main"] = map[string]any{} }} {
			ev := stgNote(stgClone(good))
			change(&ev)
			if _, _, err := h.check(ev); err == nil {
				t.Fatal("untrusted CI observer accepted")
			}
		}
		f := review684Fork(t, h)
		f.add(stgClone(good))
	})
	t.Run("interval_and_replay", func(t *testing.T) {
		t.Parallel()
		f := review684Fork(t, h)
		before := f.state().Batches[stgTestBatch]
		proof := f.add(stgClone(good))
		f.reject(stgClone(good), "duplicate")
		if !reflect.DeepEqual(before, f.state().Batches[stgTestBatch]) {
			t.Fatal("observation changed batch state")
		}
		f.add(stgUnblock(block, "inputs_changed", proof.Seq))
		f.reject(review684CIObservation(f, block, "late"), "transition")
		second := f.block("ci_red")
		f.reject(review684CIObservation(f, block, "late"), "transition")
		f.reject(stgUnblock(second, "inputs_changed", proof.Seq), "transition")
		f.reject(review684CIObservation(f, second, "green"), "transition")
		fresh := f.add(review684CIObservation(f, second, "second-green"))
		f.add(stgUnblock(second, "inputs_changed", fresh.Seq))
	})
}

func TestReview684LatestProofBindsCompleteCIIdentity(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"old_infra_with_new_uid_present", "uid_repurposed", "stale_acceptance", "latest"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			m := stgTestMember()
			h := stgReady(t, m)
			block := h.block("ci_red")
			ci := h.ci("latest-ci")
			ci["ci"].(map[string]any)["created_at"] = block.At.Format(time.RFC3339Nano)
			ci["ci"].(map[string]any)["infra_commit"] = stgSHA("f")
			proof := h.add(ci)
			h.add(stgUnblock(block, "inputs_changed", proof.Seq))
			h.add(memberRecord(m, "released", block.Seq))
			h.settle(m)
			review684OpenNext(h, m)
			accepted := review684Accept(proof, 2)
			if mutation == "uid_repurposed" {
				accepted["ci"].(map[string]any)["infra_commit"] = stgSHA("d")
			}
			if mutation == "stale_acceptance" {
				accepted["ci"].(map[string]any)["read_at"] = block.At.Format(time.RFC3339Nano)
			}
			h.add(accepted)
			snap := stgForBatch(h.snapshot(1, m), 2)
			if mutation == "old_infra_with_new_uid_present" {
				h.add(stgForBatch(h.ci("old-infra-ci"), 2))
			} else {
				review684Input(snap, "infraRevision", accepted["ci"].(map[string]any)["infra_commit"].(string))
			}
			h.add(snap)
			start := stgForBatch(h.start(1), 2)
			if mutation == "latest" {
				h.add(start)
			} else {
				review684Refuse(t, h, start, "permit reused inputs")
			}
		})
	}
}

func TestReview684NewerProofWinsReverseCloseOrder(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	first := h.block("baseline_moved")
	second := h.block("diff_unlanded")
	e1 := h.add(stgTestEvidence(h.at, "cluster/cicd/manual-1", "external", "manual-1"))
	e2 := h.add(stgTestEvidence(h.at, "cluster/cicd/manual-2", "external", "manual-2"))
	h.add(stgUnblock(second, "external_release", e2.Record.String("evidence")))
	h.add(stgUnblock(first, "external_release", e1.Record.String("evidence")))
	h.add(memberRecord(m, "released", second.Seq))
	h.settle(m)
	for _, proof := range []StgEvent{e1, e2} {
		t.Run(proof.Record.String("evidence"), func(t *testing.T) {
			t.Parallel()
			f := review684Fork(t, h)
			review684OpenNext(f, m)
			f.add(stgForBatch(f.ci("next"), 2))
			snap := stgForBatch(f.snapshot(1, m), 2)
			snap["base"] = review684Base(proof)
			f.add(snap)
			if proof.Seq == e2.Seq {
				f.add(stgForBatch(f.start(1), 2))
			} else {
				review684Refuse(t, f, stgForBatch(f.start(1), 2), "permit reused the baseline")
			}
		})
	}
}

func TestReview684CISelectionChecksKnownObservations(t *testing.T) {
	t.Parallel()
	h := stgReady(t, stgTestMember())
	block := h.block("ci_red")
	first := h.add(review684CIObservation(h, block, "first"))
	second := h.add(review684CIObservation(h, block, "second"))
	review684Refuse(t, h, stgUnblock(block, "inputs_changed", first.Seq), "CI predates an already recorded selection")
	h.add(stgUnblock(block, "inputs_changed", second.Seq))
	for _, tc := range []struct {
		name    string
		change  func(map[string]any)
		allowed bool
	}{
		{"different_results", func(r map[string]any) { r["ci"].(map[string]any)["ui_commit"] = stgSHA("f") }, false},
		{"later_name", func(r map[string]any) { r["ci"].(map[string]any)["name"] = "z-run" }, false},
		{"first_name", func(r map[string]any) { r["ci"].(map[string]any)["name"] = "a-run" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := review684Fork(t, h)
			b := f.block("ci_red")
			c1 := f.add(review684CIObservation(f, b, "tie-1"))
			r := review684CIObservation(f, b, "tie-2")
			r["ci"].(map[string]any)["created_at"] = c1.Record.object("ci").String("created_at")
			tc.change(r)
			if tc.allowed {
				p := f.add(r)
				f.add(stgUnblock(b, "inputs_changed", p.Seq))
			} else {
				review684Refuse(t, f, r, "CI selection is ambiguous or loses the name tie-break")
				f.add(stgUnblock(b, "inputs_changed", c1.Seq))
			}
		})
	}
}
