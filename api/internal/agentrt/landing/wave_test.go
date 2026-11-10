package landing

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"testing"
)

func TestWaveExcluded(t *testing.T) {
	for _, tc := range []struct {
		repo, path       string
		urgent, excluded bool
	}{
		{"bifrost-platform", "api/internal/probe/check.go", false, false},
		{"bifrost-platform", "console/src/components/Example.tsx", false, false},
		{"bifrost-platform", "api/internal/probe/check.go", true, true},
		{"bifrost-platform", ".github/workflows/ci.yaml", false, true},
		{"bifrost-trade-infra", "k8s/workload.yaml", false, true},
		{"bifrost-platform", rules.S5Paths()[0], false, true},
		{"bifrost-platform", "api/internal/secrets/key.go", false, true},
		{"bifrost-platform", "api/internal/permissions/check.go", false, true},
		{"bifrost-platform", "api/internal/rbac/check.go", false, true},
		{"bifrost-platform", "api/internal/schema/data.sql", false, true},
		{"bifrost-platform", "api/internal/migrations/001.sql", false, true},
	} {
		t.Run(tc.repo+"/"+tc.path, func(t *testing.T) {
			c := rules.LandingChange{Kind: "M", OldPath: tc.path, NewPath: tc.path, OldMode: "100644", NewMode: "100644"}
			if got := WaveExcluded(tc.repo, []rules.LandingChange{c}, tc.urgent); (got != "") != tc.excluded {
				t.Fatalf("exclusion %q, want excluded %v", got, tc.excluded)
			}
			if tc.excluded && !tc.urgent {
				c.Kind = "R"
				c.NewPath = "api/internal/probe/check.go"
				if WaveExcluded(tc.repo, []rules.LandingChange{c}, false) == "" {
					t.Fatal("rename escaped exclusion")
				}
			}
		})
	}
}
