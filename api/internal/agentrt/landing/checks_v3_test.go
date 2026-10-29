package landing

import (
	"reflect"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

func TestPolicyChecksLayerOrderAndCoreClassification(t *testing.T) {
	t.Parallel()
	now := epoch.Add(time.Hour)
	for name, mutate := range map[string]func(*Facts){
		"missing":       func(f *Facts) { f.Rule = nil },
		"kind":          func(f *Facts) { f.Rule.Kind = "other" },
		"body":          func(f *Facts) { f.Rule.Landing = nil },
		"tenant":        func(f *Facts) { f.Rule.Landing.Tenant = "other" },
		"not effective": func(f *Facts) { f.Rule.Effective = now.Add(time.Second) },
		"expired":       func(f *Facts) { f.Rule.Expires = now },
		"repo removed":  func(f *Facts) { f.Rule.Landing.Repos = nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := eligibleFacts()
			if reason, soft := PolicyChecks(f, now); reason != "" || len(soft) != 0 {
				t.Fatal("control", reason, soft)
			}
			mutate(&f)
			if reason, soft := PolicyChecks(f, now); reason != "rule" || len(soft) != 0 {
				t.Fatal(reason, soft)
			}
			// Absolute/structural is checked before the bad rule, never softened.
			f.Changes[0].NewPath = ".env.local"
			if reason, _ := PolicyChecks(f, now); reason != "floor" {
				t.Fatal("masked absolute", reason)
			}
		})
	}
	for _, path := range []string{"api/internal/agentrt/store/zz_new.go", "api/internal/server/x.go", "console/src/pages/ApprovalsPage.tsx", "mcp/platform/src/x.ts", "config/platform-auth.yaml", "api/internal/statefile/x.go", ".gitattributes"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := eligibleFacts()
			f.Rule.Landing.Repos[0].Allow = []string{"**"}
			if reason, soft := PolicyChecks(f, now); reason != "" || len(soft) != 0 {
				t.Fatal("control", reason, soft)
			}
			f.Changes[0].NewPath, f.Changes[0].OldPath = path, path
			reason, soft := PolicyChecks(f, now)
			if reason != "" || !reflect.DeepEqual(soft, []rules.LandingRefusal{rules.LandingCore}) {
				t.Fatal(reason, soft)
			}
		})
	}
}

func TestLandingV1CompatibilityAcrossClaimAndPush(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, reason string
		mutate       func(*Facts)
	}{
		{"floor", "floor", func(f *Facts) { f.Changes[0].NewPath = "api/internal/research/x.go" }},
		{"rule paths", "rule", func(f *Facts) { f.Changes[0].NewPath = "README.md" }},
		{"presentation", "rule", func(f *Facts) { f.Changes[0].AddedText = "fetch('/x')" }},
		{"size", "rule", func(f *Facts) { f.Changes[0].Added = 301 }},
		{"quota", "limit", func(f *Facts) { f.ReservedToday = 5 }},
		{"in flight", "limit", func(f *Facts) { f.OtherInFlight = 1 }},
		{"missing rule", "rule", func(f *Facts) { f.Rule = nil }},
		{"structural", "floor", func(f *Facts) { f.Changes[0].NewMode = "120000" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, gate := range []func(Facts, time.Time) Decision{EligibleToClaim, EligibleToPush} {
				f := eligibleFacts()
				now := epoch.Add(time.Hour)
				if d := gate(f, now); !d.Allowed {
					t.Fatal("v1 control", d)
				}
				tc.mutate(&f)
				if d := gate(f, now); d.Allowed || d.Reason != tc.reason {
					t.Fatal(d)
				}
			}
		})
	}
}
