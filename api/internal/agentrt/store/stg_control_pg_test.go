package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Serial: real PG fixtures and FreezeEventClock.
func TestStgControlPGCompareAppendAndNone(t *testing.T) {
	for _, mode := range []string{"enabled", "none", "stale"} {
		t.Run(mode, func(t *testing.T) {
			x := b5Ready(t)
			s := x.read(t)
			if mode == "none" {
				x.none(t)
			}
			seq := s.LastSeq
			if mode == "stale" {
				seq--
			}
			records := []store.StgRecord{store.StgRecord(b5Record("diag", "batch", "try", 1, "what", "ci_waiting", "gen", 1))}
			changed, err := store.CommitStgControl(ctx, x.w.rt, "dev", func() bool { return true }, seq, records)
			ok(t, err)
			after := x.read(t)
			if mode == "enabled" {
				if !changed || after.LastSeq != s.LastSeq+1 {
					t.Fatal("enabled compare-and-append failed")
				}
			} else if changed || after.LastSeq != s.LastSeq {
				t.Fatal("disabled/stale writer appended", changed, after.LastSeq, s.LastSeq)
			}
		})
	}
}

// Serial: FreezeEventClock; separate connections race on the stream CAS.
func TestStgControlPGTwoConnectionsOnlyOneDecision(t *testing.T) {
	x := b5Ready(t)
	s := x.read(t)
	second := x.w.db.Connect(t, x.w.db.Runtime)
	type result struct {
		did bool
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	rs := []store.StgRecord{store.StgRecord(b5Record("diag", "batch", "try", 1, "what", "ci_waiting", "gen", 1))}
	for _, db := range []store.DB{x.w.rt, second} {
		safego.Go("test-stg-control-cas", func() {
			<-start
			did, err := store.CommitStgControl(context.Background(), db, "dev", func() bool { return true }, s.LastSeq, rs)
			results <- result{did, err}
		})
	}
	close(start)
	count := 0
	for range 2 {
		select {
		case r := <-results:
			ok(t, r.err)
			if r.did {
				count++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("stream writers deadlocked")
		}
	}
	if count != 1 || x.read(t).LastSeq != s.LastSeq+1 {
		t.Fatal("decision was not unique", count)
	}
}

// Serial: FreezeEventClock; committed archive identity is read from real PG.
func TestStgControlPGArchiveReceiptExactIdentity(t *testing.T) {
	x := b5Ready(t)
	p := x.permit(t)
	archive := stgrelease.PostgresLedger{DB: x.w.rt, Tenant: "dev"}
	id := delivery.STGRunIdentity{Namespace: "cicd", Name: p.Started.String("run"), UID: "run-uid"}
	present, err := archive.HasRunEvidence(ctx, id)
	ok(t, err)
	if present {
		t.Fatal("unarchived intent accepted")
	}
	b5Append(t, x.w, b5Record("bound", "batch", "try", 1, "uid", string(id.UID)))
	at := x.at.Add(time.Minute).Truncate(time.Microsecond)
	x.advance(at)
	b5Append(t, x.w, b5Record("run_evidence", "batch", "try", 1, "uid", string(id.UID), "status", "Failed", "raw", map[string]any{"results": []any{}, "child_refs": []any{}, "conditions": []any{}}, "at", at.Format(time.RFC3339Nano)))
	for _, mode := range []string{"exact", "name", "uid", "namespace"} {
		t.Run(mode, func(t *testing.T) {
			changed := id
			switch mode {
			case "name":
				changed.Name = "other"
			case "uid":
				changed.UID = "other"
			case "namespace":
				changed.Namespace = "other"
			}
			present, err := archive.HasRunEvidence(ctx, changed)
			ok(t, err)
			if present != (mode == "exact") {
				t.Fatal("receipt ignored identity", mode, present)
			}
		})
	}
}

// Serial: FreezeEventClock. A forged rule version cannot unblock a rule
// refusal; the same entry with the actual enabled rule is the control.
func TestStgControlPGRuleProofReadsCurrentRule(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			x := b5Ready(t)
			s := x.read(t)
			snap := s.Batches[b5Batch].Tries[1].Snapshot.Record
			base := snap["base"].(map[string]any)
			id := store.StgBlockID(commitB, base["P"].(string), "rule", "prior")
			block := b5Append(t, x.w, b5Record("blocked", "block", "block", id, "why", "rule", "P", commitB, "base", base, "first_at", x.at.Format(time.RFC3339Nano), "evidence_version", "prior", "detail", map[string]any{"rules": map[string]any{"dev": 0}}))
			x.advance(x.at.Add(time.Minute))
			s = x.read(t)
			proof := store.StgRecord(b5Record("rule_observed", "block", "block", id, "interval_seq", block.Seq, "rule_id", "landing:dev", "rule_version", version, "read_at", store.DecisionAt().Format(time.RFC3339Nano)))
			close := store.StgRecord(b5Record("unblocked", "block", "block", id, "why", "rule_changed", "ref", s.LastSeq+1))
			changed, err := store.CommitStgControl(ctx, x.w.rt, "dev", func() bool { return true }, s.LastSeq, []store.StgRecord{proof, close})
			if version == 1 {
				ok(t, err)
				if !changed || x.read(t).Blocks[id].IsOpen() {
					t.Fatal("current rule did not unblock")
				}
			} else if err == nil || changed || x.read(t).LastSeq != s.LastSeq {
				t.Fatal("forged rule proof committed")
			}
		})
	}
}

// Serial: FreezeEventClock and real PostgreSQL transactions.
func TestStgControlPGEnableFenceOnceAndNone(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			x := b5Ready(t)
			before := x.read(t)
			if disabled {
				x.none(t)
				before = x.read(t)
			}
			did, err := store.OpenStgBatch(ctx, x.w.rt, "dev", func() bool { return true })
			ok(t, err)
			after := x.read(t)
			if disabled {
				if did || after.LastSeq != before.LastSeq || !after.Legacy.STGEnabledAt.IsZero() {
					t.Fatal("none wrote an enable fence")
				}
			} else {
				if !did || after.LastSeq != before.LastSeq+1 || !after.Legacy.STGEnabledAt.Equal(x.at.Truncate(time.Microsecond)) {
					t.Fatal("missing first enable fence")
				}
				seq := after.LastSeq
				for range 3 {
					did, err = store.OpenStgBatch(ctx, x.w.rt, "dev", func() bool { return true })
					ok(t, err)
					if did || x.read(t).LastSeq != seq {
						t.Fatal("repeated enable fence or batch")
					}
				}
			}
		})
	}
}

// Serial: the policy changes after a GET/final gate read but before writing.
func TestStgControlPGNoneGuardsLateRecoveryAndGate(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint(disabled), func(t *testing.T) {
			x := b5Ready(t)
			p := x.permit(t)
			if disabled {
				x.none(t)
			}
			before := x.read(t)
			action, err := store.RecordStgControlRecovery(ctx, x.w.rt, "dev", b5Batch, 1, store.StgRunObservation{IntentSeq: p.IntentSeq, PermitSeq: p.PermitSeq, Kind: "found", UID: "late-uid", ReadAt: store.DecisionAt()})
			ok(t, err)
			if disabled {
				if action != "wait" || x.read(t).LastSeq != before.LastSeq {
					t.Fatal("none admitted late recovery")
				}
			} else if action != "bound" || x.read(t).LastSeq != before.LastSeq+1 {
				t.Fatal("enabled late recovery missing")
			}
		})
	}
	x := b5Ready(t)
	x.src.ControlTenant = "dev"
	observe := x.src.Observe
	x.src.Observe = func(c context.Context, r store.StgRecord) (store.StgGateObservation, error) {
		o, e := observe(c, r)
		x.none(t)
		return o, e
	}
	before := x.read(t).LastSeq
	_, d, err := store.AuthorizeStgRelease(ctx, x.w.rt, b5Batch, 1, x.src, nil)
	ok(t, err)
	if d.Allowed || d.Why != "disabled" || x.read(t).LastSeq != before {
		t.Fatal("none final gate wrote a release refusal", d)
	}
}

// Serial: the pending member is a real landed root with a fresh main proof.
func TestStgControlPGAdmissionFromLandedMember(t *testing.T) {
	w, root, op, src, prepared := liveReady(t)
	at := prepared.Add(10 * time.Minute).Truncate(time.Microsecond)
	t.Cleanup(store.FreezeEventClock(at))
	auth, d, err := store.AuthorizeLandingPush(ctx, w.rt, src, op, observed(prepared))
	ok(t, err)
	if !d.Allowed {
		t.Fatal(d)
	}
	_, err = store.RecordPushResult(ctx, w.rt, auth, "ok")
	ok(t, err)
	ok(t, appendLanding(w, landingRepoStream, newLandingRecord(op, "landed", "R", commitB)))
	s, err := store.ReadStgRelease(ctx, w.rt)
	ok(t, err)
	o := s.Legacy.Operations[op]
	candidate := store.MainCandidate{Op: op, Root: root, Phase: o.Phase, Outcome: o.Outcome, R: o.R, M0: o.M0, A: o.A}
	_, err = store.ReconcileStgMainOne(ctx, w.rt, "dev", "bifrost-platform", candidate, store.MainEvidence{Head: commitB, HasR: true, Validity: "effective", ReadAt: at})
	ok(t, err)
	for range 2 {
		did, err := store.OpenStgBatch(ctx, w.rt, "dev", func() bool { return true })
		ok(t, err)
		if !did {
			t.Fatal("fence/admission did not progress")
		}
	}
	s, err = store.ReadStgRelease(ctx, w.rt)
	ok(t, err)
	m := store.StgMemberKey{Root: root, Op: op, R: commitB}
	if s.LastBatch != 1 || s.Batches[b5Batch].Members[m] != 1 || !s.Members[m].FirstPendingAt.Equal(at) {
		t.Fatal("pending member missing or clock reset")
	}
}
