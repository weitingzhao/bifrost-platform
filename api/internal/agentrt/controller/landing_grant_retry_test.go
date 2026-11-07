package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func grantRetryFacts(at time.Time) landing.Facts {
	return landing.Facts{Repo: "bifrost-platform", Rule: &rules.Rule{
		Header: rules.Header{Effective: at.Add(-time.Hour), Expires: at.Add(time.Hour)},
		Landing: &rules.Landing{Repos: []rules.LandingRepo{{Repo: "bifrost-platform"}},
			Limits: rules.LandingLimits{MaxLandingsPerDay: 5, MaxInFlightPerRepo: 1}},
	}}
}

func TestGrantedLandingRetryCurrentCapacity(t *testing.T) {
	at := time.Now()
	for _, name := range []string{"released between scans", "replacement still active", "quota exhausted", "both full", "repo limit zero", "expired rule", "missing rule", "removed repo"} {
		t.Run(name, func(t *testing.T) {
			var g grantTries
			g.due("op/grant", at)
			g.record("op/grant", landing.Decision{Reason: "limit"}, nil)
			f := grantRetryFacts(at)
			switch name {
			case "replacement still active":
				f.OtherInFlight = 1
			case "quota exhausted":
				f.ReservedToday = 5
			case "both full":
				f.OtherInFlight, f.ReservedToday = 1, 5
			case "repo limit zero":
				f.Rule.Landing.Repos[0].Limits = &rules.LandingRepoLimits{MaxInFlight: 0}
			case "expired rule":
				f.Rule.Expires = at
			case "missing rule":
				f.Rule = nil
			case "removed repo":
				f.Rule.Landing.Repos = nil
			}
			ready, err := g.ready("op/grant", func() time.Time { return at.Add(15 * time.Second) }, func(reason string) (bool, error) {
				if reason != "limit" {
					t.Fatal(reason)
				}
				return grantCapacityAvailable(f, at), nil
			})
			if err != nil || ready != (name == "released between scans") {
				t.Fatal("incorrect capacity wakeup", ready, err)
			}
			if ready && g.due("op/grant", at.Add(30*time.Second)) {
				t.Fatal("one release scheduled two attempts")
			}
		})
	}
}

func TestGrantedLandingRetryKeepsOtherBackoffs(t *testing.T) {
	at := time.Now()
	for _, reason := range []string{"ci", "rule", "grant_evidence", "grant_insufficient", "stale", "error", "allowed", "rule-recheck"} {
		t.Run(reason, func(t *testing.T) {
			var g grantTries
			g.due("key", at)
			if reason != "rule-recheck" {
				g.record("key", landing.Decision{Reason: "limit"}, nil)
				var err error
				if reason == "error" {
					err = errors.New("unreadable")
				}
				g.record("key", landing.Decision{Reason: reason, Allowed: reason == "allowed"}, err)
			}
			for _, elapsed := range []time.Duration{grantTryEvery - time.Second, grantTryEvery} {
				ready, err := g.ready("key", func() time.Time { return at.Add(elapsed) }, func(string) (bool, error) {
					t.Fatal("non-capacity refusal was probed")
					return true, nil
				})
				if err != nil || ready != (elapsed == grantTryEvery) {
					t.Fatal("lost ten-minute backoff", ready, err)
				}
			}
		})
	}
}

func TestGrantedLandingRetryProbeErrorBackoff(t *testing.T) {
	for _, reason := range []string{"limit", "window"} {
		t.Run(reason, func(t *testing.T) {
			at := time.Now()
			clock := func() time.Time { return at }
			var g grantTries
			g.due("key", at)
			g.record("key", landing.Decision{Reason: reason}, nil)
			unreadable := errors.New("unreadable")
			ready, err := g.ready("key", clock, func(string) (bool, error) {
				at = at.Add(2 * time.Minute) // slow failure
				return true, unreadable
			})
			if ready || !errors.Is(err, unreadable) {
				t.Fatal(ready, err)
			}
			failedAt := at
			for _, elapsed := range []time.Duration{grantTryEvery - time.Second, grantTryEvery} {
				at = failedAt.Add(elapsed)
				ready, err = g.ready("key", clock, func(string) (bool, error) {
					t.Fatal("probe repeated during error backoff")
					return true, nil
				})
				if err != nil || ready != (elapsed == grantTryEvery) {
					t.Fatal(ready, err)
				}
			}
		})
	}
}

func TestGrantedLandingRetrySharesWindowProbe(t *testing.T) {
	at := time.Now()
	clock := func() time.Time { return at }
	var g grantTries
	for _, key := range []string{"a/grant", "b/grant"} {
		g.due(key, at)
		g.record(key, landing.Decision{Reason: "window"}, nil)
	}
	reads := 0
	// Keep closed past the fallback interval: no full attempt every 10m.
	for scan := 0; scan < 45; scan++ {
		at = at.Add(15 * time.Second)
		var window grantWindowProbe
		probe := func(string) (bool, error) {
			return window.ready(context.Background(), func(context.Context, string) (bool, error) {
				reads++
				return scan < 44, nil
			}, "bifrost-platform")
		}
		for _, key := range []string{"a/grant", "b/grant"} {
			ready, err := g.ready(key, clock, probe)
			if err != nil || ready != (scan == 44) {
				t.Fatal("window wait", scan, key, ready, err)
			}
		}
	}
	if reads != 45 {
		t.Fatal("waiting grants did not share the window probe", reads)
	}
}

// Only an operation past its CI holds the slot against a granted landing,
// in the probe as in the final gate: one in its CI or parked does not.
func TestGrantedLandingProbeCountsOnlyMain(t *testing.T) {
	at := time.Now()
	for _, c := range []struct {
		phase, outcome string
		holds          bool
	}{{"claimed", "", false}, {"branch_pushed", "", false}, {"parked", "", false}, {"awaiting_grant", "", false},
		{"main_intent", "", true}, {"main_result", "ok", true}, {"main_result", "unknown", true}, {"main_result", "non_ff", false},
		{"failed", "bound", false}, {"discarded", "main_changed", false}} {
		t.Run(c.phase+"/"+c.outcome, func(t *testing.T) {
			s := landing.State{Operations: map[string]landing.Operation{
				"land:a:1": {Task: "a", Phase: "awaiting_grant"},
				"land:b:1": {Task: "b", Phase: c.phase, Outcome: c.outcome},
			}}
			f := grantRetryFacts(at)
			f.OtherInFlight = grantSlotHolders(s, "land:a:1")
			if grantCapacityAvailable(f, at) == c.holds {
				t.Fatal("capacity", f.OtherInFlight)
			}
		})
	}
}

// A grant refused for the limit while another operation holds main_intent
// is tried in the pass after that operation ends; its CI-bound neighbour does
// not keep it waiting.
func TestGrantedLandingTriedAfterMainIntentEnds(t *testing.T) {
	at := time.Now()
	clock := func() time.Time { return at }
	var g grantTries
	s := landing.State{Operations: map[string]landing.Operation{
		"land:a:1": {Task: "a", Phase: "awaiting_grant"},
		"land:b:1": {Task: "b", Phase: "branch_pushed"},
		"land:c:1": {Task: "c", Phase: "main_intent"},
	}}
	g.observe("bifrost-platform", s)
	if !g.due("land:a:1/grant_1", at) {
		t.Fatal("a new grant is not due at once")
	}
	g.record("land:a:1/grant_1", landing.Decision{Reason: "limit"}, nil)
	probe := func(string) (bool, error) {
		f := grantRetryFacts(at)
		f.OtherInFlight = grantSlotHolders(s, "land:a:1")
		return grantCapacityAvailable(f, at), nil
	}
	at = at.Add(15 * time.Second)
	g.observe("bifrost-platform", s)
	if ready, err := g.ready("land:a:1/grant_1", clock, probe); ready || err != nil {
		t.Fatal("tried while C holds main_intent", ready, err)
	}
	s.Operations["land:c:1"] = landing.Operation{Task: "c", Phase: "failed", Outcome: "bound"}
	at = at.Add(15 * time.Second)
	g.observe("bifrost-platform", s)
	if ready, err := g.ready("land:a:1/grant_1", clock, probe); !ready || err != nil {
		t.Fatal("not tried the pass after C ended", ready, err)
	}
}

// A change of the repository stream (an operation moved, a grant recorded)
// clears the ten-minute wait of its operations' grants, as drillBackoff does
// with drillGeneration; the same stream keeps it, and the rule rechecks
// without a grant keep theirs.
func TestGrantedLandingStreamChangeClearsWait(t *testing.T) {
	at := time.Now()
	s := landing.State{Operations: map[string]landing.Operation{
		"land:a:1": {Task: "a", Phase: "awaiting_grant"},
		"land:b:1": {Task: "b", Phase: "branch_pushed"},
	}}
	var g grantTries
	g.observe("bifrost-platform", s)
	for _, key := range []string{"land:a:1/grant_1", "land:a:1/rule:1", "land:x:1/grant_9"} {
		if !g.due(key, at) {
			t.Fatal("first try not due", key)
		}
		g.record(key, landing.Decision{Reason: "ci"}, nil)
	}
	waiting := func(key string) bool { return !g.due(key, at) }
	at = at.Add(time.Minute)
	g.observe("bifrost-platform", s)
	if !waiting("land:a:1/grant_1") {
		t.Fatal("an unchanged stream cleared the wait")
	}
	moved := landing.State{Operations: map[string]landing.Operation{
		"land:a:1": {Task: "a", Phase: "awaiting_grant"},
		"land:b:1": {Task: "b", Phase: "discarded", Outcome: "main_changed"},
	}}
	at = at.Add(time.Minute)
	g.observe("bifrost-platform", moved)
	if waiting("land:a:1/grant_1") {
		t.Fatal("an operation moved and the grant still waits")
	}
	if !waiting("land:a:1/rule:1") || !waiting("land:x:1/grant_9") {
		t.Fatal("the stream change cleared a rule recheck or another repository's grant")
	}
	g.record("land:a:1/grant_1", landing.Decision{Reason: "ci"}, nil)
	granted := landing.State{Operations: moved.Operations, Grants: map[string][]landing.Interval{"land:a:1": {{Start: at, End: at.Add(time.Hour)}}}}
	at = at.Add(time.Minute)
	g.observe("bifrost-platform", granted)
	if waiting("land:a:1/grant_1") {
		t.Fatal("a grant recorded and the grant still waits")
	}
}
