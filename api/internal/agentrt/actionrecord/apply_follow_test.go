package actionrecord

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// 2026-11-20, CI lane step 7: apply records said done with an empty check
// though the run could still fail. The record now carries the run's end.
// Positive control: a record that did not start a run is left as it was.
func TestAnApplyRecordCarriesItsRunsEnd(t *testing.T) {
	base := Record{Outcome: OutcomeDone}
	reads := func(seq ...[3]any) RunOutcome {
		i := 0
		return func(context.Context, string) (bool, bool, string, error) {
			s := seq[min(i, len(seq)-1)]
			i++
			if e, ok := s[2].(error); ok {
				return false, false, "", e
			}
			return s[0].(bool), s[1].(bool), s[2].(string), nil
		}
	}
	got := AfterApplyRun(context.Background(), base, "apply-x", reads([3]any{false, false, ""}, [3]any{true, true, ""}), time.Millisecond, time.Second)
	if got.Outcome != OutcomeDone || got.CheckResult != "apply run apply-x succeeded" {
		t.Fatalf("succeeded run: %+v", got)
	}
	got = AfterApplyRun(context.Background(), base, "apply-x", reads([3]any{true, false, "Forbidden"}), time.Millisecond, time.Second)
	if got.Outcome != OutcomeFailed || !strings.Contains(got.CheckResult, "Forbidden") {
		t.Fatalf("failed run: %+v", got)
	}
	got = AfterApplyRun(context.Background(), base, "apply-x", reads([3]any{false, false, ""}), time.Millisecond, 5*time.Millisecond)
	if got.Outcome != OutcomeDone || !strings.Contains(got.CheckResult, "still running") {
		t.Fatalf("running run: %+v", got)
	}
	got = AfterApplyRun(context.Background(), base, "apply-x", reads([3]any{false, false, errors.New("boom")}), time.Millisecond, 5*time.Millisecond)
	if !strings.Contains(got.CheckResult, "could not be read") {
		t.Fatalf("unreadable run: %+v", got)
	}
	refused := Record{Outcome: OutcomeRefused}
	if got := AfterApplyRun(context.Background(), refused, "", reads([3]any{true, true, ""}), time.Millisecond, time.Second); got != refused {
		t.Fatalf("a call that started no run changed: %+v", got)
	}
}
