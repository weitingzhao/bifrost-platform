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
