package landing

// TD-447 1, 2, 6: which latest operations leave k+1 due, the born-terminal
// successor and the phases at main (review 645, Codex; adopted by the coordinator).

import (
	"reflect"
	"testing"
	"time"
)

func pendingApply(t *testing.T, s *State, at time.Time, r Record) {
	t.Helper()
	if err := s.Apply(Entry{At: at, Record: r}); err != nil {
		t.Fatal(err)
	}
}

func pendingBorn(op string) Record {
	r := v11("failed", "why", "bound")
	r["op"] = op
	return r
}

func TestAwaitsNextSuccessors(t *testing.T) {
	for _, phase := range []string{"discarded", "non_ff", "unknown_non_ff", "bound", "stale", "ci_red"} {
		t.Run(phase, func(t *testing.T) {
			var s State
			first := epoch
			switch phase {
			case "discarded":
				s = mustFold(t, phaseRecords("discarded", true))
			case "non_ff", "unknown_non_ff":
				s = mustFold(t, phaseRecords("main_intent", false))
				if phase == "unknown_non_ff" {
					pendingApply(t, &s, epoch.Add(7*time.Minute), record("main_result", "outcome", "unknown"))
				}
				pendingApply(t, &s, epoch.Add(8*time.Minute), record("main_result", "outcome", "non_ff"))
			default:
				s = mustFold(t, phaseRecords("claimed", true))
				pendingApply(t, &s, epoch.Add(time.Minute), record("failed", "why", phase))
				first = epoch.Add(time.Hour)
				pendingApply(t, &s, first, v11("retry", "root", task1, "after_op", "land:1", "reason", "explicit", "by", "owner"))
			}
			before := s.Operations["land:1"]
			if !s.AwaitsNext(task1) || !s.RootPending(task1) {
				t.Fatal("pending root lost")
			}
			next := "land:" + task1 + ":2"
			if s.Apply(Entry{At: first.Add(6*time.Hour - time.Nanosecond), Record: pendingBorn(next)}) == nil {
				t.Fatal("early bound accepted")
			}
			pendingApply(t, &s, first.Add(6*time.Hour), pendingBorn(next))
			o := s.Operations[next]
			if s.LatestOperation(task1) != next || o.Phase != "failed" || o.Outcome != "bound" || o.Retried || o.Active() || !o.BlocksClaim() || s.RootPending(task1) {
				t.Fatal("bad successor", o)
			}
			if o.Task != before.Task || o.Attempt != before.Attempt || o.A != before.A || o.RuleVersion != before.RuleVersion || o.DryRun != before.DryRun {
				t.Fatal("lost inherited binding")
			}
			if !reflect.DeepEqual(before, s.Operations["land:1"]) {
				t.Fatal("predecessor changed")
			}
			budget, n := RootBudget(s, task1)
			if !budget.Equal(first) || n != 2 || o.ClaimNumber != 2 {
				t.Fatal("budget changed", budget, n)
			}
			if retry, ok := s.Retries[task1]; ok && (retry.ConsumedBy != next || retry.AfterOp != "land:1" || !retry.At.Equal(first)) {
				t.Fatal("retry changed incorrectly", retry)
			}
			if s.Apply(Entry{At: first.Add(7 * time.Hour), Record: pendingBorn("land:" + task1 + ":3")}) == nil {
				t.Fatal("repeat settlement minted another successor")
			}
		})
	}
}

func TestBornBoundSuccessorCanBeRetriedAndClaimed(t *testing.T) {
	s := mustFold(t, phaseRecords("failed", true))
	pendingApply(t, &s, epoch.Add(time.Hour), v11("retry", "root", task1, "after_op", "land:1", "reason", "explicit", "by", "owner"))
	next := "land:" + task1 + ":2"
	pendingApply(t, &s, epoch.Add(7*time.Hour), pendingBorn(next))
	stale := v11("retry", "root", task1, "after_op", "land:1", "reason", "old", "by", "owner")
	if s.Apply(Entry{At: epoch.Add(8 * time.Hour), Record: stale}) == nil {
		t.Fatal("old answer revived")
	}
	r := v11("retry", "root", task1, "after_op", next, "reason", "new", "by", "owner")
	r["op"] = next
	pendingApply(t, &s, epoch.Add(8*time.Hour), r)
	if !s.RootPending(task1) {
		t.Fatal("new retry not pending")
	}
	claim := v11("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1)
	claim["op"] = "land:" + task1 + ":3"
	pendingApply(t, &s, epoch.Add(8*time.Hour+time.Minute), claim)
	first, n := RootBudget(s, task1)
	if n != 3 || !first.Equal(epoch.Add(8*time.Hour)) || s.Retries[task1].ConsumedBy != claim.String("op") {
		t.Fatal(first, n, s.Retries)
	}
	if Bound(first, n, first.Add(time.Hour)) || !Bound(first, 8, first.Add(time.Minute)) {
		t.Fatal("budget reset incorrectly")
	}
}

func TestRehearsedAndAtMainPhaseMatrix(t *testing.T) {
	s := mustFold(t, phaseRecords("rehearsed", true))
	if s.RootPending(task1) || s.AwaitsNext(task1) {
		t.Fatal("rehearsal pending")
	}
	if s.Apply(Entry{At: epoch.Add(7 * time.Hour), Record: pendingBorn("land:" + task1 + ":2")}) == nil {
		t.Fatal("rehearsal failed by bound")
	}
	claim := v11("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1)
	claim["op"] = "land:" + task1 + ":2"
	pendingApply(t, &s, epoch.Add(time.Hour), claim) // common protocol still permits production
	for _, phase := range []string{"claimed", "prepared", "branch_intent", "branch_pushed", "parked", "main_intent", "ok", "denied", "unknown", "non_ff", "landed", "reverted"} {
		t.Run(phase, func(t *testing.T) {
			p := phase
			if phase == "ok" || phase == "denied" || phase == "unknown" || phase == "non_ff" {
				p = "main_intent"
			}
			s := mustFold(t, phaseRecords(p, false))
			if p != phase {
				pendingApply(t, &s, epoch.Add(time.Hour), record("main_result", "outcome", phase))
			}
			o := s.Operations["land:1"]
			err := s.Apply(Entry{At: epoch.Add(7 * time.Hour), Record: record("failed", "why", "bound")})
			if (err == nil) != (o.EndsAtBound() || phase == "unknown") {
				t.Fatal("transition mismatch", o, err)
			}
			wantMain := phase == "main_intent" || phase == "ok" || phase == "denied" || phase == "unknown"
			if o.AtMain() != wantMain {
				t.Fatal("AtMain mismatch", o)
			}
		})
	}
	// STG is a separate operation with no root binding, not landed -> stg_*.
	s = mustFold(t, phaseRecords("landed", false))
	if s.Apply(Entry{At: epoch.Add(time.Hour), Record: record("stg_pending", "R", []any{R})}) == nil {
		t.Fatal("landed reused as stg operation")
	}
	r := record("stg_pending", "R", []any{R})
	r["op"] = "stg:1"
	pendingApply(t, &s, epoch.Add(time.Hour), r)
	if s.Operations["stg:1"].Task != "" || s.Operations["stg:1"].EndsAtBound() || s.RootPending(task1) {
		t.Fatal("STG acquired root binding")
	}
	r = record("failed", "why", "bound")
	r["op"] = "stg:1"
	if s.Apply(Entry{At: epoch.Add(7 * time.Hour), Record: r}) == nil {
		t.Fatal("STG failed through landing transition")
	}
}
