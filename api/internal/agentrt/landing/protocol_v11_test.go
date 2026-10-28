package landing

import (
	"slices"
	"testing"
	"time"
)

func v11(kind string, kv ...any) Record { r := record(kind, kv...); r["v"] = Version; return r }
func phaseRecords(phase string, dry bool) []Record {
	rs := records()[:4]
	if dry {
		rs[1]["dry_run"] = true
		ref := "refs/heads/land/stg/" + task1 + "/1"
		rs[2]["ref"], rs[3]["ref"] = ref, ref
	}
	switch phase {
	case "empty":
		return nil
	case "claimed":
		return rs[:1]
	case "prepared":
		return rs[:2]
	case "branch_intent":
		return rs[:3]
	case "branch_pushed":
		return rs
	case "parked":
		return append(rs, record("parked", "why", "window", "retry_at", epoch.Add(time.Hour).Format(time.RFC3339)))
	case "failed":
		return append(rs, record("failed", "why", "bound"))
	case "discarded":
		return append(rs, v11("discarded", "why", "main_changed"))
	}
	rs = append(rs, record("ci_seen", "R", R, "run", "ci-green", "status", "Succeeded", "created_at", epoch.Add(2*time.Minute).Format(time.RFC3339)))
	if phase == "green" {
		return rs
	}
	if phase == "rehearsed" {
		return append(rs, v11("rehearsed", "R", R))
	}
	rs = append(rs, record("main_intent", "R", R, "M0", M0))
	if phase == "main_intent" {
		return rs
	}
	rs = append(rs, record("main_result", "outcome", "ok"))
	if phase == "main_result" {
		return rs
	}
	rs = append(rs, record("landed", "R", R))
	if phase == "landed" {
		return rs
	}
	return append(rs, record("reverted", "R", R, "by", A))
}
func TestV11TransitionMatrix(t *testing.T) {
	phases := []string{"empty", "claimed", "prepared", "branch_intent", "branch_pushed", "green", "parked", "failed", "discarded", "rehearsed", "main_intent", "main_result", "landed", "reverted"}
	for _, phase := range phases {
		for _, kind := range []string{"discarded", "rehearsed", "retry"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				dry := !slices.Contains([]string{"main_intent", "main_result", "landed", "reverted"}, phase)
				s := mustFold(t, phaseRecords(phase, dry))
				r := v11("discarded", "why", "main_changed")
				want := slices.Contains([]string{"branch_intent", "branch_pushed", "green", "parked"}, phase)
				if kind == "rehearsed" {
					r = v11(kind, "R", R)
					want = phase == "green"
				}
				if kind == "retry" {
					r = v11(kind, "root", task1, "after_op", "land:1", "reason", "owner retry", "by", "owner")
					want = phase == "failed"
				}
				err := s.Apply(Entry{At: epoch.Add(time.Hour), Record: r})
				if (err == nil) != want {
					t.Fatalf("want success %v: %v", want, err)
				}
				if want && kind != "retry" {
					o := s.Operations["land:1"]
					if o.Active() || !o.Terminal() || o.BlocksClaim() || o.EverLanded || o.Reservable(task1) {
						t.Fatal(o)
					}
				}
			})
		}
	}
}
func TestV11ClosedFieldsAndRehearsalFences(t *testing.T) {
	for _, why := range []string{"main_changed", "branch_collision"} {
		if _, e := Parse(v11("discarded", "why", why)); e != nil {
			t.Fatal(e)
		}
	}
	for _, r := range []Record{record("discarded", "why", "main_changed"), v11("discarded", "why", "stale"), v11("retry", "root", task1, "after_op", "land:1", "reason", "", "by", "owner"), v11("rehearsed", "R", "wrong")} {
		if _, e := Parse(r); e == nil {
			t.Fatal("open fields", r)
		}
	}
	for _, mut := range []func(*State){
		func(s *State) { o := s.Operations["land:1"]; o.DryRun = false; s.Operations["land:1"] = o },
		func(s *State) {
			o := s.Operations["land:1"]
			o.Rerun = true
			o.RerunAt = epoch.Add(3 * time.Minute)
			s.Operations["land:1"] = o
		},
		func(s *State) { o := s.Operations["land:1"]; o.CI["status"] = "Failed"; s.Operations["land:1"] = o },
	} {
		s := mustFold(t, phaseRecords("green", true))
		mut(&s)
		if s.Apply(Entry{At: epoch.Add(time.Hour), Record: v11("rehearsed", "R", R)}) == nil {
			t.Fatal("unsafe rehearsal")
		}
	}
	s := mustFold(t, phaseRecords("green", true))
	if s.Apply(Entry{At: epoch.Add(time.Hour), Record: v11("rehearsed", "R", A)}) == nil {
		t.Fatal("wrong R")
	}
}
func TestV11RetryBudgetAndDecline(t *testing.T) {
	for _, why := range reasons {
		if why == "main_lost" {
			continue // ends main_result{ok} alone (TestMainLostOnlyFromOk)
		}
		rs := phaseRecords("failed", true)
		rs[len(rs)-1]["why"] = why
		s := mustFold(t, rs)
		retry := v11("retry", "root", task1, "after_op", "land:1", "reason", "explicit", "by", "owner")
		err := s.Apply(Entry{At: epoch.Add(7 * time.Hour), Record: retry})
		want := slices.Contains([]string{"bound", "stale", "ci_red"}, why)
		if (err == nil) != want {
			t.Fatal(why, err)
		}
		if !want {
			continue
		}
		if s.Apply(Entry{At: epoch.Add(7 * time.Hour), Record: retry}) == nil {
			t.Fatal("double retry")
		}
		first, n := RootBudget(s, task1)
		if !first.Equal(epoch.Add(7*time.Hour)) || n != 1 {
			t.Fatal(first, n)
		}
		claim := v11("claimed", "task", task1, "attempt", attempt1, "A", A, "rule_version", 1)
		claim["op"] = "land:2"
		if err := s.Apply(Entry{At: epoch.Add(7*time.Hour + time.Minute), Record: claim}); err != nil {
			t.Fatal(err)
		}
		if s.Retries[task1].ConsumedBy != "land:2" {
			t.Fatal("unconsumed retry")
		}
		if s.Apply(Entry{At: epoch.Add(8 * time.Hour), Record: retry}) == nil {
			t.Fatal("old failure retried")
		}
		if s.Apply(Entry{At: epoch.Add(8 * time.Hour), Record: v11("failed", "why", "declined")}) == nil {
			t.Fatal("declined old operation")
		}
		if Bound(first, n, epoch.Add(8*time.Hour)) {
			t.Fatal("retry did not restart clock")
		}
		if !Bound(first, 8, epoch.Add(8*time.Hour)) {
			t.Fatal("attempt budget reset")
		}
	}
	s := mustFold(t, phaseRecords("failed", true))
	if e := s.Apply(Entry{At: epoch.Add(time.Hour), Record: v11("failed", "why", "declined")}); e != nil {
		t.Fatal(e)
	}
	if e := s.Apply(Entry{At: epoch.Add(2 * time.Hour), Record: v11("retry", "root", task1, "after_op", "land:1", "reason", "explicit", "by", "owner")}); e == nil {
		t.Fatal("declined retried")
	}
}
