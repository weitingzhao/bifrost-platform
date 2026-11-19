package coord

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/problemloop"
)

// 2026-11-19, OWNER_LAW #36: the coordinator judged two real problems fixed at
// their source and the loop still counted them open. A real problem closes when
// handled or fixed at the source; a false one only when its source is fixed.
func TestARealProblemFixedAtItsSourceIsClosed(t *testing.T) {
	cases := []ProblemCase{
		{Verdict: problemloop.VerdictReal, Outcome: problemloop.OutcomeSourceFixed},
		{Verdict: problemloop.VerdictReal, Outcome: problemloop.OutcomeHandled},
		{Verdict: problemloop.VerdictReal, Outcome: problemloop.OutcomeOpen},
		{Verdict: problemloop.VerdictFalse, Outcome: problemloop.OutcomeHandled},
		{Verdict: problemloop.VerdictFalse, Outcome: problemloop.OutcomeSourceFixed},
	}
	n := countProblems(cases)
	want := []bool{true, true, false, false, true}
	for i, c := range cases {
		if c.Closed != want[i] {
			t.Errorf("%s/%s: closed %v, want %v", c.Verdict, c.Outcome, c.Closed, want[i])
		}
	}
	if n.Closed != 3 || n.Open != 2 {
		t.Fatalf("counts %+v", n)
	}
}
