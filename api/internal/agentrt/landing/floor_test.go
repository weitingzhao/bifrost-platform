package landing

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// CUT §11.6 v2: a change under the immutable floor ends the root (floor),
// before and whatever the rule; a path the rule merely does not allow only
// pauses it (rule). An operation already claimed ends failed{floor}.
func TestFloorEndsTheRootRulePausesIt(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	f := eligibleFacts()
	f.Changes = []rules.LandingChange{{OldPath: "api/main.go", NewPath: "api/main.go", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1}}
	if d := EligibleToClaim(f, now); d.Allowed || d.Reason != "floor" {
		t.Fatalf("floor: %+v", d)
	}
	f.Rule = nil // v1.2: rule validity precedes the soft support decision.
	if d := EligibleToClaim(f, now); d.Reason != "rule" {
		t.Fatalf("floor without a rule: %+v", d)
	}
	g := eligibleFacts()
	g.Changes = []rules.LandingChange{{OldPath: "console/src/pages/X.tsx", NewPath: "console/src/pages/X.tsx", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p/>"}}
	if d := EligibleToClaim(g, now); d.Allowed || d.Reason != "rule" {
		t.Fatalf("outside the allow list only pauses: %+v", d)
	}
	for name, c := range map[string]rules.LandingChange{
		"binary":       {OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Binary: true},
		"symlink":      {NewPath: "console/src/components/L.tsx", NewMode: "120000", Added: 1},
		"no path":      {Added: 1},
		".env":         {NewPath: "console/.env.local", NewMode: "100644", Added: 1},
		"package.json": {OldPath: "console/package.json", NewPath: "console/package.json", OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1},
	} {
		if _, floor := rules.FloorViolation("bifrost-platform", []rules.LandingChange{c}); !floor {
			t.Errorf("%s: not the floor", name)
		}
	}
	if why, floor := rules.FloorViolation("bifrost-platform", eligibleFacts().Changes); floor {
		t.Fatal("an allowed change read as the floor:", why)
	}
}
