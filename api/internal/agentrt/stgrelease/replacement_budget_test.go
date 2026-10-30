package stgrelease

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func TestControlReplacementRetainsBudgetTrigger(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Failed", "Succeeded"} {
		for _, attempts := range []int{1, 3} {
			for _, all := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/attempts=%d/all=%t", status, attempts, all), func(t *testing.T) {
					t.Parallel()
					c, l, r := replacementBudgetFixture(t, attempts, status, all)
					commits := l.commits
					pass(t, c) // replacement check, budget interval and settlement share one CAS
					if l.commits != commits+1 {
						t.Fatal("replacement settlement was split across commits")
					}
					s := l.state(t)
					b := s.Batches[leaseBatch]
					if b.Phase != "CLOSED" || s.Members[controlMember].To != "covered" || len(s.DeploymentChecks) != 1 {
						t.Fatal("replacement preconditions failed", b.Phase, s.Members, s.DeploymentChecks)
					}
					wantVerify := "run_failed"
					if status == "Succeeded" {
						wantVerify = "external_release"
					}
					if b.Tries[b.Try].Verify.Record.String("why") != wantVerify || b.Tries[b.Try].Retry.Seq != 0 || r.creates != 0 {
						t.Fatal("verification changed or execution retried")
					}
					seq := s.LastSeq
					for range 100 {
						pass(t, c)
					}
					if l.state(t).LastSeq != seq {
						t.Fatal("settlement replay appended duplicate records")
					}
					wantOther := "covered"
					if !all {
						wantOther = "released"
						if attempts == 3 {
							wantOther = "exhausted"
						}
					}
					if s.Members[reviewOther].To != wantOther || s.Budget[store.StgBudgetKey{R: reviewOther.R, Gen: 1}] != int64(attempts) {
						t.Fatal("unexpected member outcome or debit", s.Members, s.Budget)
					}
					blocks := 0
					for _, block := range s.Blocks {
						if block.IsOpen() {
							if block.Opened.Record.String("why") != "budget_exhausted" {
								t.Fatal("trusted replacement must not create baseline_moved")
							}
							blocks++
						}
					}
					if attempts == 3 && !all {
						m := memberValue(reviewOther)
						m["gen"] = 1
						_, err := l.Commit(context.Background(), seq, []store.StgRecord{batchRecord("opened", "stg:bifrost-platform:2", 0, "members", []any{m}, "first_pending_at", map[string]any{reviewOther.R: s.Members[reviewOther].FirstPendingAt.Format(time.RFC3339Nano)})})
						if err == nil {
							t.Fatal("exhausted member unexpectedly readmitted")
						}
						t.Logf("re-entry rejected: %v", err)
						if blocks != 1 {
							t.Fatalf("trusted replacement settled uncovered member as exhausted without budget trigger: member=%s budget=%d phase=%s blocks=%d; 100 passes made no progress", s.Members[reviewOther].To, s.Budget[store.StgBudgetKey{R: reviewOther.R, Gen: 1}], b.Phase, blocks)
						}
						replacementBudgetDecision(t, c, l, "retry_release")
					} else if blocks != 0 {
						t.Fatal("all covered or remaining budget must not block", blocks)
					}
				})
			}
		}
	}
}
