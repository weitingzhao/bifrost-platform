package coord

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/problemloop"
)

// 2026-11-19, LAW-8: the coordinator judged two real problems fixed at
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

// 2026-11-19 (the coordinator): the loop showed time to verdict but not to
// closed. Time to closed is over the closed cases only; an open case's
// verdict does not count.
func TestTimeToClosedIsOverTheClosedCasesOnly(t *testing.T) {
	at := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	after := func(m int) *time.Time { v := at.Add(time.Duration(m) * time.Minute); return &v }
	cases := []ProblemCase{
		{Letter: at, VerdictAt: after(30), Verdict: problemloop.VerdictReal, Outcome: problemloop.OutcomeHandled},
		{Letter: at, VerdictAt: after(90), Verdict: problemloop.VerdictReal, Outcome: problemloop.OutcomeSourceFixed},
		{Letter: at, VerdictAt: after(10), Verdict: problemloop.VerdictReal, Outcome: problemloop.OutcomeOpen},
	}
	n := countProblems(cases)
	if n.ClosedMedianMinutes == nil || *n.ClosedMedianMinutes != 60 {
		t.Fatalf("closed median = %v, want 60", n.ClosedMedianMinutes)
	}
	if none := countProblems([]ProblemCase{{Letter: at}}); none.ClosedMedianMinutes != nil {
		t.Fatalf("no closed case: closed median = %v, want nil", *none.ClosedMedianMinutes)
	}
}
