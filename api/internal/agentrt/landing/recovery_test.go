package landing

import (
	"fmt"
	"testing"
	"time"
)

func TestRerunIdentityDecisionAndFold(t *testing.T) {
	intent := epoch.Add(10*time.Minute + 100*time.Millisecond)
	for _, tc := range []struct {
		name, run string
		created   time.Time
		ready     bool
	}{
		{"same second", CIName("land:1", 2), intent.Truncate(time.Second), true},
		{"old success", "old-green", intent.Add(-time.Minute), false},
		{"other attempt", CIName("land:1", 1), intent.Add(time.Second), false},
		{"other operation", CIName("land:other", 2), intent.Add(time.Second), false},
		{"old exact name", CIName("land:1", 2), intent.Add(-time.Minute), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := drillState(t)
			if err := s.Apply(Entry{At: intent, Record: v11("ci_rerun", "try", 2)}); err != nil {
				t.Fatal(err)
			}
			obs := observation()
			obs.Candidate.Run, obs.Candidate.CreatedAt = tc.run, tc.created
			now := intent.Add(time.Minute)
			if got := NextDrill(s, "land:1", obs, now); (got.Kind == "ready") != tc.ready {
				t.Fatal("decision", got)
			}
			if err := s.Apply(Entry{At: now, Record: v11("ci_seen", "run", tc.run, "status", "Succeeded", "R", R, "created_at", tc.created.Format(time.RFC3339Nano))}); err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(Entry{At: now, Record: v11("rehearsed", "R", R)}); (err == nil) != tc.ready {
				t.Fatal("fold", err)
			}
		})
	}
}

func TestBornTerminalBoundRequiresDiscardedSuccessor(t *testing.T) {
	for _, tc := range []struct {
		name, phase, why, op string
		at                   time.Time
		v                    any
		want                 bool
	}{
		{"elapsed", "discarded", "bound", fmt.Sprintf("land:%s:2", task1), epoch.Add(7 * time.Hour), Version, true},
		{"not elapsed", "discarded", "bound", fmt.Sprintf("land:%s:2", task1), epoch.Add(time.Hour), Version, false},
		{"wrong number", "discarded", "bound", fmt.Sprintf("land:%s:3", task1), epoch.Add(7 * time.Hour), Version, false},
		{"wrong root", "discarded", "bound", "land:other:2", epoch.Add(7 * time.Hour), Version, false},
		{"no predecessor", "empty", "bound", fmt.Sprintf("land:%s:1", task1), epoch.Add(7 * time.Hour), Version, false},
		{"old version", "discarded", "bound", fmt.Sprintf("land:%s:2", task1), epoch.Add(7 * time.Hour), 1, false},
		{"other reason", "discarded", "stale", fmt.Sprintf("land:%s:2", task1), epoch.Add(7 * time.Hour), Version, false},
		{"active", "branch_pushed", "bound", fmt.Sprintf("land:%s:2", task1), epoch.Add(7 * time.Hour), Version, false},
		{"failed", "failed", "bound", fmt.Sprintf("land:%s:2", task1), epoch.Add(7 * time.Hour), Version, false},
		{"immutable", "discarded", "bound", "land:1", epoch.Add(7 * time.Hour), Version, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := mustFold(t, phaseRecords(tc.phase, true))
			r := v11("failed", "why", tc.why)
			r["v"], r["op"] = tc.v, tc.op
			err := s.Apply(Entry{At: tc.at, Record: r})
			if (err == nil) != tc.want {
				t.Fatal(err)
			}
			if tc.want {
				o := s.Operations[tc.op]
				if o.Task != task1 || o.Active() || !o.BlocksClaim() || o.ClaimNumber != 2 || o.Ref != "" || s.Operations["land:1"].Phase != "discarded" {
					t.Fatal(o)
				}
				if s.Apply(Entry{At: tc.at, Record: r}) == nil {
					t.Fatal("duplicate born-terminal event accepted")
				}
			}
		})
	}
}
