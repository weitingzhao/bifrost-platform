package landing

import (
	"context"
	"errors"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

const A = "1111111111111111111111111111111111111111"
const R = "2222222222222222222222222222222222222222"
const M0 = "3333333333333333333333333333333333333333"
const U = "4444444444444444444444444444444444444444"
const task1 = "t_01ARZ3NDEKTSV4RRFFQ69G5FAV"
const attempt1 = "a_01ARZ3NDEKTSV4RRFFQ69G5FAV"

func eligibleFacts() Facts {
	c := rules.LandingChange{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p>Ready</p>"}
	return Facts{Environment: "prod", Mode: "live", Tenant: "dev", Repo: "bifrost-platform", Task: task1, Attempt: attempt1, A: A, Base: M0, MissionState: "active", StepState: "active", TaskState: "accepted", SingleCommit: true, DependenciesEffective: true, AcceptedAt: epoch.Add(time.Minute), MissionCreated: epoch, EnabledAt: epoch, History: MainHistory{State: Never}, Changes: []rules.LandingChange{c}, DeployChanges: []rules.LandingChange{c}, Rule: &rules.Rule{Header: rules.Header{ID: "landing:dev", Version: 1, Effective: epoch.Add(-12 * time.Hour), Expires: epoch.Add(30 * 24 * time.Hour)}, Kind: rules.KindLanding, Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", Allow: []string{"console/src/components/**/*.tsx"}, STGRelease: "bifrost-deliver-platform"}}, Limits: rules.LandingLimits{MaxFiles: 10, MaxChangedLines: 300, MaxInFlightPerRepo: 1}}}}
}
func TestLandingStageSeparation(t *testing.T) {
	f := eligibleFacts()
	now := epoch.Add(time.Hour)
	if !EligibleToClaim(f, now).Allowed {
		t.Fatal("positive claim")
	}
	f.History = MainHistory{State: Effective, Commit: R}
	f.SnapshotHistory = f.History
	f.Operation = Operation{R: R, EverLanded: true}
	f.Terminal = true
	if EligibleToClaim(f, now).Allowed {
		t.Fatal("landed result claimed twice")
	}
	if !EligibleToDeploy(f, now).Allowed {
		t.Fatal("landed result cannot deploy")
	}
	d := EligibleToPush(f, now)
	if d.Allowed || !d.Reconcile {
		t.Fatal("own result must reconcile")
	}
	f.Rule = nil
	if !Reconcile(f).Allowed || EligibleToDeploy(f, now).Allowed {
		t.Fatal("revocation affects wrong stage")
	}
}
func TestLandingActualEnableFence(t *testing.T) {
	for _, name := range []string{"old mission new acceptance", "accepted after midnight before observation"} {
		t.Run(name, func(t *testing.T) {
			f := eligibleFacts()
			if name == "old mission new acceptance" {
				f.MissionCreated = epoch.Add(-time.Second)
			} else {
				f.AcceptedAt = epoch.Add(-time.Hour)
			}
			if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
				t.Fatal("pre-enable work admitted")
			}
		})
	}
	f := eligibleFacts()
	f.Rule.Version = 2
	f.Rule.Effective = epoch.Add(30 * time.Minute)
	if !EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("new version discarded queued work")
	}
}

// Any used plan of the mission may land its goal tasks, not only the latest
// one adopted (R5:5.7: a second plan staled every task of the first).
func TestLandingAnyUsedPlan(t *testing.T) {
	f := eligibleFacts()
	f.GoalTask = true
	f.ProducingPlan = "old"
	f.UsedPlans = []string{"old"}
	if !EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("used plan denied")
	}
	f.UsedPlans = []string{"new", "old"}
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal("older used plan denied", d.Reason)
	}
	f.UsedPlans = []string{"new"}
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Allowed || d.Reason != "stale" {
		t.Fatal("never used plan admitted", d)
	}
	f.UsedPlans = nil
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Allowed || d.Reason != "stale" {
		t.Fatal("unknown adoption admitted", d)
	}
	// Proposed directly by the coordinator: no producing plan to be used.
	f.ProducingPlan = ""
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal("directly proposed goal task denied", d.Reason)
	}
	f.UsedPlans = []string{"new"}
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal("directly proposed goal task denied", d.Reason)
	}
}
func TestLandingHistoryFailClosed(t *testing.T) {
	for _, h := range []History{Unknown, "", Reverted, Effective} {
		f := eligibleFacts()
		f.History.State = h
		if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
			t.Fatal("unknown/present history admitted", h)
		}
	}
	reads := 0
	read := func(context.Context, string) (Facts, error) {
		reads++
		return eligibleFacts(), errors.New("history unavailable")
	}
	d, e := Evaluate(context.Background(), read, task1, "claim", epoch)
	if e == nil || d.Allowed {
		t.Fatal("read failure became never")
	}
	_, _ = Evaluate(context.Background(), read, task1, "push", epoch)
	if reads != 2 {
		t.Fatal("facts cached")
	}
	d, e = Evaluate(context.Background(), read, task1, "reconcile", epoch)
	if e != nil || !d.Reconcile || reads != 2 {
		t.Fatal("reconcile depends on eligibility")
	}
}
func TestLandingEligibilityRefusals(t *testing.T) {
	cases := map[string]func(*Facts){"no_merge": func(f *Facts) { f.NoMerge = true }, "drop_step": func(f *Facts) { f.StepState = "dropped" }, "stopped_step": func(f *Facts) { f.StepState = "stopped" }, "mission stopped": func(f *Facts) { f.MissionState = "stopped" }, "dependency reverted": func(f *Facts) { f.DependenciesEffective = false }, "multiple commits": func(f *Facts) { f.SingleCommit = false }, "terminal": func(f *Facts) { f.Terminal = true }, "wrong environment": func(f *Facts) { f.Environment = "stg" }, "expired": func(f *Facts) { f.Rule.Expires = epoch }, "missing rule": func(f *Facts) { f.Rule = nil }, "not accepted": func(f *Facts) { f.TaskState = "submitted" }}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			f := eligibleFacts()
			mut(&f)
			if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed || EligibleToPush(f, epoch.Add(time.Hour)).Allowed {
				t.Fatal("admitted")
			}
		})
	}
	f := eligibleFacts()
	f.Operation = Operation{R: R, EverLanded: true}
	f.SnapshotHistory = MainHistory{State: Effective, Commit: R}
	f.DeployChanges[0].NewPath = "api/internal/actions/tier.go"
	if EligibleToDeploy(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("whole deployment diff not checked")
	}
}
func TestLandingBoundsIncludeWindowWait(t *testing.T) {
	if Bound(epoch, 7, epoch.Add(6*time.Hour-time.Nanosecond)) {
		t.Fatal("too early")
	}
	if !Bound(epoch, 1, epoch.Add(6*time.Hour)) || !Bound(epoch, 8, epoch.Add(time.Minute)) {
		t.Fatal("total bound ignored")
	}
}

func TestLandingRuleLimitsAtEachGate(t *testing.T) {
	f := eligibleFacts()
	f.OtherInFlight = 1
	if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed || EligibleToPush(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("in-flight limit ignored")
	}
	// The daily quota is gone (cleanup wave 1): a legacy max_landings_per_day
	// of zero holds neither gate back.
	f = eligibleFacts()
	f.Rule.Landing.Limits.LegacyMaxLandingsPerDay = 0
	if !EligibleToClaim(f, epoch.Add(time.Hour)).Allowed || !EligibleToPush(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("the daily quota still refuses")
	}
}

// Open and allowed changes proceed regardless of size or grant enablement.
// A deny match is still refused as rule.
func TestLandingOpenLaneMayAwaitGrant(t *testing.T) {
	// Open and outside the floor: a refusal of it is rule, never floor.
	const open = "console/vitest.config.ts"
	now := epoch.Add(time.Hour)
	for _, tc := range []struct {
		name, path string
		deny       []string
		enabled    bool
		want       string
	}{
		{"open oversized", open, nil, true, "allowed"},
		{"grants not enabled", open, nil, false, "allowed"},
		{"open under deny", open, []string{open}, true, "rule"},
		{"no open path", "console/src/components/A.tsx", nil, true, "allowed"},
	} {
		f := eligibleFacts()
		f.GrantEnabled = tc.enabled
		f.Changes[0].OldPath, f.Changes[0].NewPath, f.Changes[0].Added = tc.path, tc.path, 400
		f.Rule.Landing.Repos[0].Deny = tc.deny
		claim, push := EligibleToClaim(f, now), EligibleToPush(f, now)
		switch tc.want {
		case "allowed":
			if !claim.Allowed || !push.Allowed {
				t.Fatal(tc.name, claim, push)
			}
		default:
			if claim.Allowed || claim.Reason != tc.want || push.Allowed {
				t.Fatal(tc.name, claim, push)
			}
		}
	}
	f := eligibleFacts()
	f.GrantEnabled = true
	f.Changes[0].OldPath, f.Changes[0].NewPath = open, open
	if !EligibleToClaim(f, now).Allowed || !EligibleToPush(f, now).Allowed {
		t.Fatal("open change within limits")
	}
}

func TestLargeLandingAndInFlightCapacity(t *testing.T) {
	t.Parallel()
	f := eligibleFacts()
	f.Changes = nil
	for i := 0; i < 200; i++ {
		c := eligibleFacts().Changes[0]
		c.OldPath = fmt.Sprintf("console/src/components/Large%d.tsx", i)
		c.NewPath = c.OldPath
		c.Added, c.Deleted = 50, 0
		f.Changes = append(f.Changes, c)
	}
	if soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes); len(soft) != 0 {
		t.Fatal(soft)
	}
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal(d)
	}
	f.OtherInFlight = 1
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Allowed {
		t.Fatal("default capacity exceeded", d)
	}
	f.Rule.Landing.Limits.MaxInFlightPerRepo = 2
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); !d.Allowed {
		t.Fatal("second claim", d)
	}
	f.OtherInFlight = 2
	if d := EligibleToClaim(f, epoch.Add(time.Hour)); d.Allowed {
		t.Fatal("capacity exceeded", d)
	}
}
