package store_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	clocktesting "k8s.io/utils/clock/testing"
)

// A separate mission owns the reversing result, so B4 must close the
// original mission without first delivering the reversal's mission.
func b5fReversal(t *testing.T, w *world, at time.Time) string {
	t.Helper()
	w.goalMission(t, "M-4")
	root := s66Root(t, w, "M-4", platformRepo)
	op, p := "land:"+root+":1", strings.Repeat("f", 40)
	ref := "refs/heads/land/prod/" + root + "/1"
	for i, r := range []map[string]any{
		newLandingRecord(op, "claimed", "task", root, "attempt", landingAttempt, "A", commitA, "rule_version", 1),
		newLandingRecord(op, "prepared", "R", p, "M0", commitB, "committer_date", at.Add(time.Second).Format(time.RFC3339Nano), "dry_run", false),
		newLandingRecord(op, "branch_intent", "ref", ref, "R", p),
		newLandingRecord(op, "branch_pushed", "ref", ref, "R", p),
		newLandingRecord(op, "ci_seen", "run", "reversal-green", "status", "Succeeded", "R", p, "created_at", at.Add(3*time.Second).Format(time.RFC3339Nano)),
		newLandingRecord(op, "main_intent", "R", p, "M0", commitB),
		newLandingRecord(op, "main_result", "outcome", "ok"),
		newLandingRecord(op, "landed", "R", p),
	} {
		store.FreezeEventClock(at.Add(time.Duration(i) * time.Second))
		ok(t, appendLanding(w, landingRepoStream, r))
	}
	return p
}

// Serial: real B4 CloseMissions, PostgreSQL now() anchor and FreezeEventClock.
func TestStgB4PGReversalBeforeAndAfterAdmission(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		for _, legal := range []bool{false, true} {
			t.Run(fmt.Sprintf("admitted=%t/legal=%t", admitted, legal), func(t *testing.T) {
				w, at := b5fWorld(t, "enabled")
				root := s66Landed(t, w, "M-3", platformRepo, at)
				op := "land:" + root + ":1"
				m := store.StgMemberKey{Root: root, Op: op, R: commitB}
				if admitted {
					store.FreezeEventClock(at.Add(time.Minute))
					b5StateFixture(t, w, root, op, store.DecisionAt(), false)
				}
				p := strings.Repeat("f", 40)
				if legal {
					p = b5fReversal(t, w, at.Add(2*time.Minute))
				}
				store.FreezeEventClock(at.Add(3 * time.Minute))
				s, err := store.ReadStgRelease(ctx, w.rt)
				ok(t, err)
				o := s.Legacy.Operations[op]
				_, err = store.ReconcileStgMainOne(ctx, w.rt, "dev", platformRepo, store.MainCandidate{Op: op, Root: root, R: commitB, A: o.A, M0: o.M0, Phase: o.Phase, Outcome: o.Outcome}, store.MainEvidence{Head: p, HasR: true, Validity: "reverted", RevertedBy: p, ReadAt: store.DecisionAt()})
				ok(t, err)
				s, err = store.ReadStgRelease(ctx, w.rt)
				ok(t, err)
				if !s.Legacy.Operations[op].Reverted {
					t.Fatal("reversal not recorded by production reconciliation")
				}
				if legal && admitted {
					var ref int64
					for _, r := range s.Records {
						if r.Record.String("kind") == "reverted" && r.Record.String("op") == op {
							ref = r.Seq
						}
					}
					b5Append(t, w, b5Record("member", "batch", "root", root, "op", op, "R", commitB, "to", "disposed", "ref", ref))
					b5Append(t, w, b5Record("settled", "batch", "members", []any{map[string]any{"root": root, "op": op, "R": commitB}}, "why", "members_settled"))
				}
				view, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
				ok(t, err)
				if len(view.Roots) != 1 || (view.Roots[0].Class == "disposed") != legal || (view.Roots[0].STG.Phase == "disposed") != legal {
					t.Fatal("wrong reversal classification", view.Roots)
				}
				want := "active"
				if legal {
					want = "stopped"
				}
				if state := closePass(t, w, at.Add(4*time.Minute)); state != want {
					t.Fatal("B4 did not honor reversal authority", state, want)
				}
				closed, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
				ok(t, err)
				if legal && (closed.Closed == nil || closed.Closed.Disposed != 1 || closed.Closed.Delivered != 0) {
					t.Fatal("disposal missing from durable close", closed.Closed)
				}
				if !legal && closed.Closed != nil {
					t.Fatal("manual reversal closed mission")
				}
				if state := closePass(t, w, at.Add(5*time.Minute)); state != want {
					t.Fatal("repeat changed state")
				}
				again, err := store.ReadMissionLandingStatus(ctx, w.rt, "M-3")
				ok(t, err)
				if !reflect.DeepEqual(closed.Closed, again.Closed) {
					t.Fatal("repeat rewrote closing record")
				}
				s, err = store.ReadStgRelease(ctx, w.rt)
				ok(t, err)
				if !admitted {
					if _, exists := s.Members[m]; exists {
						t.Fatal("B4 fabricated an STG admission")
					}
				}
			})
		}
	}
}

// Serial: expires_at is anchored to whole-second PostgreSQL now(), with the
// enabled/mode fences before mission creation (b5fWorld's missing-rule mode).
func TestStgControlPGExpiredRuleBaseline(t *testing.T) {
	for _, phase := range []string{"OPEN", "SNAP"} {
		for _, baseline := range []string{"known", "missing", "ambiguous"} {
			if phase == "SNAP" && baseline == "missing" {
				continue
			} // protocol requires a trusted snapshot base
			t.Run(phase+"/"+baseline, func(t *testing.T) {
				w, at := b5fWorld(t, "missing")
				root := s66Landed(t, w, "M-3", platformRepo, at)
				op := "land:" + root + ":1"
				store.FreezeEventClock(at.Add(time.Minute))
				if baseline != "missing" {
					b5StateFixture(t, w, root, op, store.DecisionAt(), phase == "SNAP")
					if baseline == "ambiguous" {
						b5fEvidence(t, w, store.DecisionAt(), "cluster/cicd/conflicting")
					}
				} else {
					b5Append(t, w, b5Record("opened", "batch", "members", []any{map[string]any{"root": root, "op": op, "R": commitB, "gen": 1}}, "first_pending_at", map[string]any{commitB: store.DecisionAt().Format(time.RFC3339Nano)}))
				}
				if phase == "OPEN" {
					b5Append(t, w, b5Record("p_selected", "batch", "P", commitB, "at", store.DecisionAt().Format(time.RFC3339Nano)))
				}
				at = at.Add(2 * time.Hour)
				store.FreezeEventClock(at)
				rule, err := store.StgControlRule(ctx, w.rt, "dev")
				ok(t, err)
				if rule == nil || rule.Expires.After(at) {
					t.Fatal("fixture rule must actually be expired")
				}
				c := &stgrelease.Controller{DB: w.rt, Tenant: "dev", Clock: clocktesting.NewFakeClock(at), Policy: func() stgrelease.WindowPolicy { return stgrelease.WindowPolicy{Environment: "prod", Rule: rule} }}
				t.Cleanup(func() { ok(t, c.Close()) })
				ok(t, c.Pass(ctx))
				s, err := store.ReadStgRelease(ctx, w.rt)
				ok(t, err)
				want := "rule"
				if baseline != "known" {
					want = "baseline_unknown"
				}
				if s.Batches[b5Batch].Phase != "CLOSED" || len(s.Blocks) != 1 || len(s.Budget) != 0 {
					t.Fatal("expired refusal not committed", s.Blocks)
				}
				for _, b := range s.Blocks {
					if !b.IsOpen() || b.Opened.Record.String("why") != want || !b.Opened.Record.Time("first_at").Equal(at) {
						t.Fatal("wrong persisted blocker", b)
					}
				}
				for range 3 {
					ok(t, c.Pass(ctx))
				}
				after, err := store.ReadStgRelease(ctx, w.rt)
				ok(t, err)
				if !reflect.DeepEqual(s, after) {
					t.Fatal("repeat moved first_at or duplicated interval")
				}
			})
		}
	}
}
