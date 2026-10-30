package stgrelease

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Both production scheduling orders consume the same parsed ledger. Tries
// spend real started credits; no test edits folded budgets or member outcomes.
func TestExternalSettlementKeepsRemainingBudgetDecision(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"Failed", "Succeeded"} {
		for _, attempts := range []int{1, 3} {
			for _, all := range []bool{false, true} {
				for _, first := range []bool{false, true} {
					for _, choice := range []string{"retry_release", "do_not_release"} {
						t.Run(fmt.Sprintf("%s/tries=%d/all=%t/external_first=%t/%s", status, attempts, all, first, choice), func(t *testing.T) {
							t.Parallel()
							c, l, r := replacementBudgetFixture(t, attempts, status, all)
							for _, m := range []store.StgMemberKey{controlMember, reviewOther} {
								if m == controlMember || all {
									l.add(t, store.StgRecord{"v": "1", "scope": "ext", "kind": "covers", "evidence": "cluster/cicd/review-external", "root": m.Root, "op": m.Op, "R": m.R})
								}
							}
							c.Runs = &externalRuns{controlRuns: r}
							if first {
								externalPass(t, c)
							}
							pass(t, c)
							if !first {
								externalPass(t, c)
							}
							s := l.state(t)
							want, blocks := "released", 0
							if all {
								want = "covered"
							} else if attempts == 3 {
								want, blocks = "exhausted", 1
							}
							b := s.Batches[leaseBatch]
							if b.Phase != "CLOSED" || s.Members[controlMember].To != "covered" || s.Members[reviewOther].To != want || len(s.Blocks) != blocks {
								t.Fatal("coverage skipped remaining-member decision", b.Phase, s.Members, s.Blocks)
							}
							if b.Tries[b.Try].Verify.Seq == 0 || b.Tries[b.Try].Retry.Seq != 0 || r.creates != 0 {
								t.Fatal("terminal verification lost or another try started")
							}
							for range 10 {
								externalPass(t, c)
								pass(t, c)
							}
							if !reflect.DeepEqual(s, l.state(t)) {
								t.Fatal("repeat scheduling changed interval identity, first_at or settlement")
							}
							if blocks == 1 {
								replacementBudgetDecision(t, c, l, choice)
							}
						})
					}
				}
			}
		}
	}
}
