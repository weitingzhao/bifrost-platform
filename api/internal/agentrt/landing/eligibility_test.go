package landing

import (
	"context"
	"errors"
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
	return Facts{Environment: "prod", Mode: "live", Tenant: "dev", Repo: "bifrost-platform", Task: task1, Attempt: attempt1, A: A, Base: M0, MissionState: "active", StepState: "active", TaskState: "accepted", SingleCommit: true, DependenciesEffective: true, AcceptedAt: epoch.Add(time.Minute), MissionCreated: epoch, EnabledAt: epoch, History: MainHistory{State: Never}, Changes: []rules.LandingChange{c}, DeployChanges: []rules.LandingChange{c}, Rule: &rules.Rule{Header: rules.Header{ID: "landing:dev", Version: 1, Effective: epoch.Add(-12 * time.Hour), Expires: epoch.Add(30 * 24 * time.Hour)}, Kind: rules.KindLanding, Landing: &rules.Landing{Tenant: "dev", Repos: []rules.LandingRepo{{Repo: "bifrost-platform", Allow: []string{"console/src/components/**/*.tsx"}, STGRelease: "bifrost-deliver-platform"}}, Limits: rules.LandingLimits{MaxFiles: 10, MaxChangedLines: 300, MaxLandingsPerDay: 5, MaxInFlightPerRepo: 1}}}}
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
func TestLandingLatestAdoptedPlan(t *testing.T) {
	f := eligibleFacts()
	f.GoalTask = true
	f.ProducingPlan = "old"
	f.LatestUsedPlan = "old"
	if !EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("still adopted plan denied")
	}
	f.LatestUsedPlan = "new"
	if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("superseded plan admitted")
	}
	f.LatestUsedPlan = ""
	if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed {
		t.Fatal("unknown adoption admitted")
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
	for _, which := range []string{"quota", "in flight"} {
		f := eligibleFacts()
		if which == "quota" {
			f.ReservedToday = 5
		} else {
			f.OtherInFlight = 1
		}
		if EligibleToClaim(f, epoch.Add(time.Hour)).Allowed || EligibleToPush(f, epoch.Add(time.Hour)).Allowed {
			t.Fatal("limit ignored", which)
		}
	}
}
