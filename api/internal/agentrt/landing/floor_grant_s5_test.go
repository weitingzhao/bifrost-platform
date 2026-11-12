package landing

import (
	"slices"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// #65 (M-402, 2026-11-11): a change of one floor-grantable Console API
// client file under a rule that enrolls S5 is TS, so S5 classifies it, but
// with grants in force it takes the grant lane: its claim and preparation
// are allowed and its final gate waits for approve_floor_landing. Only an
// S5-lane preparation refuses it, as floor.
func TestFloorGrantableConsoleAPIWaitsForAGrantUnderS5(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	p := "console/src/api/planSnapshot.ts"
	facts := func(grants bool) Facts {
		f := s5Facts()
		f.GrantEnabled = grants
		f.Changes = []rules.LandingChange{{Kind: "M", OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 3, Deleted: 1, AddedText: "export const x = 1"}}
		f.S5Patch.OldNewPathsAndModes = []S5PathMode{{p, p, "100644", "100644", "M", false}}
		return f
	}
	f := facts(true)
	soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
	if !slices.Equal(soft, []rules.LandingRefusal{rules.LandingFloorGrant, rules.LandingRulePaths}) || !rules.LandingFloorGrantable(f.Rule.Landing, f.Repo, f.Changes, soft) {
		t.Fatalf("precondition: not floor-grantable: %v", soft)
	}
	if !ClassifyS5PathsAndRelations(f, f.S5Policy).NeedsS5Evidence {
		t.Fatal("precondition: S5 does not classify the TS change")
	}
	if S5AutomaticRoute(f) || !MayAwaitGrant(f, soft) {
		t.Fatal("a floor-grantable change left the grant lane")
	}
	if d := PreparationDecision(f, now); !d.Allowed {
		t.Fatalf("claim refused a floor-grantable change: %+v", d)
	}
	// The PROD final gate (store landing_live) judges the grant lane with
	// EligibleToClaim and, with soft refusals and no grant, records
	// awaiting_grant, which raises the approve_floor_landing card.
	if d := EligibleToClaim(f, now); !d.Allowed {
		t.Fatalf("final gate refused a floor-grantable change: %+v", d)
	}
	// The S5 lane itself cannot wait for a grant: reached only without
	// grants in force (or for an operation recorded s5_required), it ends
	// the root as floor.
	off := facts(false)
	if !S5AutomaticRoute(off) {
		t.Fatal("without grants the TS change is not S5's")
	}
	if d := PreparationDecision(off, now); d.Reason != "floor" {
		t.Fatalf("without grants: %+v", d)
	}
	f.Operation.S5.Required = true
	if d := PreparationDecision(f, now); d.Reason != "floor" {
		t.Fatalf("s5_required: %+v", d)
	}
}

// #66 / #69: floor-grantable paths mixed with allowed or open paths are not
// floor-grantable (approve_floor_landing pins every changed file) and,
// without a core path, end the root as floor at claim. Current behavior,
// pending the Owner's decision in a later wave; this test records it.
func TestFloorGrantableMixedStillRefused(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	modify := func(p, text string) rules.LandingChange {
		return rules.LandingChange{Kind: "M", OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: text}
	}
	for name, changes := range map[string][]rules.LandingChange{
		"#66 with an allowed component": {modify("console/src/api/planSnapshot.ts", "export const x = 1"), modify("console/src/components/A.tsx", "<p>Ready</p>")},
		"#69 with an open path":         {modify("api/internal/metriccompute/x.go", "package metriccompute"), modify("api/internal/agentrt/controller/x.go", "package controller")},
	} {
		f := eligibleFacts()
		f.GrantEnabled = true
		f.Changes = changes
		soft := rules.LandingSoftRefusals(f.Rule.Landing, f.Repo, f.Changes)
		if !slices.Contains(soft, rules.LandingFloorGrant) || rules.LandingFloorGrantable(f.Rule.Landing, f.Repo, f.Changes, soft) {
			t.Fatalf("%s: precondition: %v", name, soft)
		}
		if d := EligibleToClaim(f, now); d.Reason != "floor" {
			t.Errorf("%s: %+v (pending the Owner: this records today's refusal)", name, d)
		}
	}
}
