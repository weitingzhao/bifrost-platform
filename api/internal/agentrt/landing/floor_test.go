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
	modify := func(p string) rules.LandingChange {
		return rules.LandingChange{OldPath: p, NewPath: p, OldMode: "100644", NewMode: "100644", Added: 1, Deleted: 1, AddedText: "<p/>"}
	}
	for name, tc := range map[string]struct {
		changes []rules.LandingChange
		why     string
	}{
		"binary":       {[]rules.LandingChange{{OldPath: "console/src/components/A.tsx", NewPath: "console/src/components/A.tsx", OldMode: "100644", NewMode: "100644", Binary: true}}, "floor"},
		"symlink":      {[]rules.LandingChange{{NewPath: "console/src/components/L.tsx", NewMode: "120000", Added: 1}}, "floor"},
		"no path":      {[]rules.LandingChange{{Added: 1}}, "floor"},
		".env":         {[]rules.LandingChange{{NewPath: "console/.env.local", NewMode: "100644", Added: 1}}, "floor"},
		"package.json": {[]rules.LandingChange{modify("console/package.json")}, "floor"},
		// The soft refusal set decides, not the floor globs: a core path
		// outside them ends the root too, and an open path under them adds
		// nothing, so a page outside allow beside it only pauses the root.
		"core outside the floor globs":       {[]rules.LandingChange{modify("console/src/pages/ApprovalsPage.tsx")}, "floor"},
		"open path and a page outside allow": {[]rules.LandingChange{modify("api/internal/cluster/x.go"), modify("console/src/pages/X.tsx")}, "rule"},
	} {
		f := eligibleFacts()
		f.Changes = tc.changes
		if d := EligibleToClaim(f, now); d.Allowed || d.Reason != tc.why {
			t.Errorf("%s: %+v, want %s", name, d, tc.why)
		}
	}
	if d := EligibleToClaim(eligibleFacts(), now); !d.Allowed {
		t.Fatal("an allowed change refused:", d)
	}
}
