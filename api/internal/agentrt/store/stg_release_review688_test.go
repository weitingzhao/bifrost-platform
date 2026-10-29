package store

import (
	"reflect"
	"testing"
	"time"
)

// Every edge uses the production append checker and a fresh Fold. Observation
// acquisition precedes persistence, so deployed.at need not follow event seq.
func TestReview688BaselineObservationOrder(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	first := h.block("baseline_moved")
	second := h.block("diff_unlanded")
	old := stgTestEvidence(h.at, "cluster/cicd/manual-old", "external", "manual-old")
	h.at = h.at.Add(time.Minute)
	newer := stgTestEvidence(h.at, "cluster/cicd/manual-new", "external", "manual-new")
	newer["content"].(map[string]any)["api_digest"] = stgDig("f")
	latest := h.add(newer)
	delayed := h.add(old)
	h.add(stgUnblock(second, "external_release", latest.Record.String("evidence")))
	h.add(stgUnblock(first, "external_release", delayed.Record.String("evidence")))
	h.add(memberRecord(m, "released", first.Seq))
	h.settle(m)
	history := h.state().BlockHistory
	for _, tc := range []struct {
		name    string
		base    StgEvent
		allowed bool
	}{
		{"newer_observation", latest, true},
		{"delayed_older_observation", delayed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := review684Fork(t, h)
			review684OpenNext(f, m)
			f.add(stgForBatch(f.ci("next"), 2))
			snap := stgForBatch(f.snapshot(1, m), 2)
			snap["base"] = review684Base(tc.base)
			f.add(snap)
			start := stgForBatch(f.start(1), 2)
			if tc.allowed {
				intent := f.add(start)
				if f.state().Batches[intent.Record.String("batch")].Phase != "STARTED" {
					t.Fatal("newest observed baseline did not reach STARTED")
				}
				f.at = intent.Record.Time("deadline")
				permit := f.add(review688Resend(f, intent))
				if f.state().Batches[intent.Record.String("batch")].Tries[1].Permit.Seq != permit.Seq || f.state().Budget[StgBudgetKey{m["R"].(string), 1}] != 1 {
					t.Fatal("resend did not renew the permit without spending again")
				}
			} else {
				review684Refuse(t, f, start, "permit reused the baseline")
			}
			if !reflect.DeepEqual(history, f.state().BlockHistory) {
				t.Fatal("baseline replacement changed interval history")
			}
		})
	}
}

func review688Resend(h *stgHarness, intent StgEvent) map[string]any {
	r := stgRecord("resend", "try", 1, "intent_seq", intent.Seq,
		"lease", map[string]any{"who": intent.Record.object("lease").String("who"), "gen": 2},
		"deadline", h.at.Add(2*time.Minute).Format(time.RFC3339Nano), "why", "not_found_unbound")
	r["batch"] = intent.Record.String("batch")
	return r
}

func TestReview688BaselineObservationTies(t *testing.T) {
	t.Parallel()
	for _, conflict := range []string{"none", "api_digest", "console_digest"} {
		t.Run(conflict, func(t *testing.T) {
			t.Parallel()
			for _, reverse := range []bool{false, true} {
				name := "forward_append"
				if reverse {
					name = "reverse_append"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					m := stgTestMember()
					h := stgReady(t, m)
					first := h.block("baseline_moved")
					second := h.block("diff_unlanded")
					a := stgTestEvidence(h.at, "cluster/cicd/manual-a", "external", "manual-a")
					b := stgTestEvidence(h.at, "cluster/cicd/manual-b", "external", "manual-b")
					// Equal instants with distinct textual timezone representations.
					b["deployed"].(map[string]any)["at"] = h.at.In(time.FixedZone("offset", 3600)).Format(time.RFC3339Nano)
					if conflict != "none" {
						b["content"].(map[string]any)[conflict] = stgDig("f")
					}
					if reverse {
						a, b = b, a
					}
					p1, p2 := h.add(a), h.add(b)
					h.add(stgUnblock(second, "evidence_changed", p2.Record.String("evidence")))
					h.add(stgUnblock(first, "external_release", p1.Record.String("evidence")))
					h.add(memberRecord(m, "released", first.Seq))
					h.settle(m)
					for _, proof := range []StgEvent{p1, p2} {
						t.Run(proof.Record.String("evidence"), func(t *testing.T) {
							t.Parallel()
							f := review684Fork(t, h)
							review684OpenNext(f, m)
							f.add(stgForBatch(f.ci("next"), 2))
							snap := stgForBatch(f.snapshot(1, m), 2)
							snap["base"] = review684Base(proof)
							f.add(snap)
							start := stgForBatch(f.start(1), 2)
							if conflict == "none" {
								intent := f.add(start)
								f.at = intent.Record.Time("deadline")
								f.add(review688Resend(f, intent))
							} else {
								review684Refuse(t, f, start, "conflicting baseline observations")
							}
						})
					}
					t.Run("strictly_newer_observation", func(t *testing.T) {
						t.Parallel()
						f := review684Fork(t, h)
						block := f.block("baseline_moved")
						latest := f.add(stgTestEvidence(f.at, "cluster/cicd/manual-latest", "external", "manual-latest"))
						f.add(stgUnblock(block, "external_release", latest.Record.String("evidence")))
						review684OpenNext(f, m)
						f.add(stgForBatch(f.ci("next"), 2))
						snap := stgForBatch(f.snapshot(1, m), 2)
						snap["base"] = review684Base(latest)
						f.add(snap)
						intent := f.add(stgForBatch(f.start(1), 2))
						f.at = intent.Record.Time("deadline")
						f.add(review688Resend(f, intent))
						if len(f.state().BlockHistory) != 3 {
							t.Fatal("superseding observations deleted history")
						}
					})
				})
			}
		})
	}
}

// Start is valid before a later blocking interval. Once that interval resolves
// against a different baseline, the frozen old snapshot cannot obtain resend.
func TestReview688ResendRejectsSupersededBaseline(t *testing.T) {
	t.Parallel()
	m := stgTestMember()
	h := stgReady(t, m)
	intent := h.add(h.start(1))
	first := h.block("baseline_moved")
	second := h.block("diff_unlanded")
	old := stgTestEvidence(h.at, "cluster/cicd/manual-old", "external", "manual-old")
	h.at = h.at.Add(time.Minute)
	newer := stgTestEvidence(h.at, "cluster/cicd/manual-new", "external", "manual-new")
	newer["content"].(map[string]any)["api_digest"] = stgDig("f")
	latest, delayed := h.add(newer), h.add(old)
	h.add(stgUnblock(second, "external_release", latest.Record.String("evidence")))
	h.add(stgUnblock(first, "external_release", delayed.Record.String("evidence")))
	before := h.state()
	review684Refuse(t, h, review688Resend(h, intent), "permit reused the baseline")
	if !reflect.DeepEqual(before, h.state()) {
		t.Fatal("refused resend changed the original permit or history")
	}
}
