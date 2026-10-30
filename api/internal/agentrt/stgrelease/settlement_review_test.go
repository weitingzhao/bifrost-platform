package stgrelease

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

var reviewOther = store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAW", Op: "land:two:1", R: sha("b")}

type reviewLateCoverLedger struct {
	*memoryLedger
	beforeCommit func()
}

func (l *reviewLateCoverLedger) Commit(ctx context.Context, seq int64, rs []store.StgRecord) (bool, error) {
	if before := l.beforeCommit; before != nil {
		l.beforeCommit = nil
		before()
	}
	return l.memoryLedger.Commit(ctx, seq, rs)
}

// Extend the input events, never the folded budget. Every test still replays
// all debits, terminal observations and backoff through the production Fold.
func reviewSecondMember(t *testing.T, l *memoryLedger) {
	t.Helper()
	for _, e := range l.entries {
		r, ok := e.Event.Body["stg_release"].(map[string]any)
		if !ok {
			continue
		}
		switch r["kind"] {
		case "opened":
			m := memberValue(reviewOther)
			m["gen"] = 1
			r["members"] = append(r["members"].([]any), m)
			r["first_pending_at"].(map[string]any)[reviewOther.R] = l.clock.Now().Format(time.RFC3339Nano)
		case "snapshot":
			r["set"] = append(r["set"].([]any), memberValue(reviewOther))
		case "started":
			r["spend"] = append(r["spend"].([]any), map[string]any{"R": reviewOther.R, "gen": 1, "n": 1})
		}
	}
	_ = l.state(t)
}

func reviewCover(t *testing.T, l *memoryLedger, m store.StgMemberKey) {
	t.Helper()
	l.effective(t, m, sha("9"))
	l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "covers", "evidence": "cluster/cicd/review-external", "root": m.Root, "op": m.Op, "R": m.R})
}

func TestControlLastDebitCoverageOrdering(t *testing.T) {
	t.Parallel()
	for _, attempts := range []int{1, 3} {
		for _, timing := range []string{"before_verify", "after_effect", "during_commit"} {
			for _, all := range []bool{false, true} {
				t.Run(fmt.Sprintf("attempts=%d/%s/all=%t", attempts, timing, all), func(t *testing.T) {
					t.Parallel()
					c, l, r, _ := controlFixture(t, "Failed")
					reviewSecondMember(t, l)
					r.workloads = workloads(dig("a"), dig("c"))
					pass(t, c)
					for i := 1; i < attempts; i++ {
						pass(t, c) // failed verify
						pass(t, c) // unchanged effect
						pass(t, c) // retry
						l.clock.Step(30 * time.Minute)
						pass(t, c) // next snapshot
						reviewStartFailed(t, l)
					}
					addCoverage := func() {
						reviewExternal(t, l, r, true)
						if all {
							reviewCover(t, l, reviewOther)
						}
						// Coverage proves prior delivery, independently of the
						// current workload effect of this unsuccessful execution.
						r.workloads = workloads(dig("a"), dig("c"))
					}
					if timing == "before_verify" {
						// The last terminal record has not been processed by the
						// controller yet; covers can arrive before its verify.
						addCoverage()
					}
					pass(t, c)
					pass(t, c)
					if timing == "after_effect" {
						addCoverage()
					}
					if timing == "during_commit" {
						c.Ledger = &reviewLateCoverLedger{memoryLedger: l, beforeCommit: addCoverage}
					}
					pass(t, c)
					if timing == "during_commit" {
						if s := l.state(t); len(s.Blocks) != 0 || s.Batches[leaseBatch].Phase == "CLOSED" {
							t.Fatal("stale budget decision survived covers committed before CAS")
						}
						pass(t, c) // Reload all members and decide from the new facts.
					}
					s := l.state(t)
					wantOther, wantBlocks := "covered", 0
					if !all {
						wantOther = "released"
						if attempts == 3 {
							wantOther, wantBlocks = "exhausted", 1
						}
					}
					if s.Members[controlMember].To != "covered" || s.Members[reviewOther].To != wantOther || s.Batches[leaseBatch].Phase != "CLOSED" || len(s.Blocks) != wantBlocks {
						t.Fatalf("member facts must precede budget: members=%v phase=%s blocks=%d", s.Members, s.Batches[leaseBatch].Phase, len(s.Blocks))
					}
					for _, m := range []store.StgMemberKey{controlMember, reviewOther} {
						if s.Budget[store.StgBudgetKey{R: m.R, Gen: 1}] != int64(attempts) {
							t.Fatal("settlement refunded a debit")
						}
					}
					for _, block := range s.Blocks {
						if !block.IsOpen() || block.Opened.Record.String("why") != "budget_exhausted" {
							t.Fatal("remaining exhausted member lost its authorization trigger")
						}
					}
					seq := s.LastSeq
					for range 5 {
						pass(t, c)
					}
					if l.state(t).LastSeq != seq {
						t.Fatal("settlement replay appended duplicate records")
					}
					// The real Fold admission checks the repository block and slot.
					fresh := store.StgMemberKey{Root: "t_01ARZ3NDEKTSV4RRFFQ69G5FAX", Op: "land:next:1", R: sha("7")}
					m := memberValue(fresh)
					m["gen"] = 1
					_, err := l.Commit(context.Background(), seq, []store.StgRecord{batchRecord("opened", "stg:bifrost-platform:2", 0, "members", []any{m}, "first_pending_at", map[string]any{fresh.R: l.clock.Now().Format(time.RFC3339Nano)})})
					if (err != nil) != (wantBlocks != 0) {
						t.Fatalf("next admission: blocks=%d err=%v", wantBlocks, err)
					}
				})
			}
		}
	}
}

func TestControlExternalEvidenceArrivalAndRecheck(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Failed", "Succeeded"} {
		for _, timing := range []string{"before_archive", "after_archive", "after_failed_verify", "after_effect"} {
			for _, covered := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/covers=%t", status, timing, covered), func(t *testing.T) {
					t.Parallel()
					c, l, r, _ := controlFixture(t, status)
					reviewSecondMember(t, l) // Other member lacks proof at external P.
					r.workloads = workloads(dig("8"), dig("9"))
					if timing == "before_archive" {
						reviewExternal(t, l, r, covered)
					}
					pass(t, c)
					l.clock.Step(16 * time.Minute)
					if timing == "after_failed_verify" || timing == "after_effect" {
						pass(t, c)
					}
					if timing == "after_effect" {
						pass(t, c)
					}
					if timing != "before_archive" {
						reviewExternal(t, l, r, covered)
					}
					for range 5 {
						pass(t, c)
					}
					s := l.state(t)
					tr := s.Batches[leaseBatch].Tries[1]
					if s.Members[controlMember].To != "covered" || s.Members[reviewOther].To != "released" || len(s.Blocks) != 0 || s.Batches[leaseBatch].Phase != "CLOSED" || tr.Retry.Seq != 0 {
						t.Fatalf("external delivery not independently consumed: members=%v phase=%s blocks=%d", s.Members, s.Batches[leaseBatch].Phase, len(s.Blocks))
					}
					if status == "Failed" && (tr.Verify.Record.String("outcome") != "failed" || tr.Verify.Record.String("why") != "run_failed") {
						t.Fatal("original failed verification rewritten")
					}
					if check := s.DeploymentChecks["cluster/cicd/review-external"]; check.Seq <= tr.RunEvidence.Seq || r.healthReads == 0 {
						t.Fatal("external deployment was not rechecked after terminal archive")
					}
					if len(s.Covers) != 1 || r.creates != 0 {
						t.Fatal("duplicate cover or retry Create")
					}
				})
			}
		}
	}
}

func TestControlFailedOtherNeedsCurrentDeployment(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"no_evidence", "digest_changed", "work_unknown", "pin_unknown", "pin_failed", "health_unknown", "health_failed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			c, l, r, _ := controlFixture(t, "Failed")
			pass(t, c)
			r.workloads = workloads(dig("8"), dig("9"))
			pass(t, c)
			pass(t, c) // effect other is durable before the recheck.
			if mode != "no_evidence" {
				reviewExternal(t, l, r, false)
			}
			switch mode {
			case "digest_changed":
				r.workloads = workloads(dig("5"), dig("6"))
			case "work_unknown":
				r.workErr = errors.New("workloads unavailable")
			case "pin_unknown":
				r.pinErr = errors.New("pin unavailable")
			case "pin_failed":
				r.pinErr = delivery.ErrSTGEvidence
			case "health_unknown":
				r.health.Outcome = "unknown"
			case "health_failed":
				r.health.Outcome = "failed"
			}
			pass(t, c)
			s := l.state(t)
			unknown := mode == "work_unknown" || mode == "pin_unknown" || mode == "health_unknown"
			if unknown {
				if len(s.Blocks) != 0 || s.Batches[leaseBatch].Phase == "CLOSED" || s.Members[controlMember].To != "" {
					t.Fatal("unreadable deployment became a definite refusal", mode)
				}
			} else if len(s.Blocks) != 1 || s.Batches[leaseBatch].Phase != "CLOSED" {
				t.Fatal("untrusted replacement did not block", mode)
			}
			if len(s.DeploymentChecks) != 0 || len(s.Covers) != 0 {
				t.Fatal("untrusted replacement acquired deployment/coverage proof", mode)
			}
		})
	}
}
