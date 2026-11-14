package landing

import (
	"slices"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

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
