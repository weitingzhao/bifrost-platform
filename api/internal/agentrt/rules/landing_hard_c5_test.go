package rules

import "testing"

// The core-level approval's material (§12.2 item 2): the §4.2 entry each
// hard_core path matches, and whether it is on the Owner's decision chain.
// hard_absolute and soft paths have neither.
func TestLandingHardCoreEntryAndDecisionChain(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		path, entry string
		chain       bool
	}{
		{"api/internal/agentrt/store/zz_new.go", "api/internal/agentrt/store/", false},
		{"api/internal/approvals/service.go", "api/internal/approvals/", true},
		{"api/internal/approvalhome/home.go", "api/internal/approvalhome/", true},
		{"api/internal/actuation/auth.go", "api/internal/actuation/", true},
		{"console/src/pages/ApprovalsPage.tsx", "console/src/pages/ApprovalsPage.tsx", true},
		{"mcp/platform/src/registerApprove.ts", "mcp/platform/", true},
		{"config/platform-auth.yaml", "config/platform-auth.yaml", false},
		{"console/sub/package.json", "**/package.json", false},
		{"deploy/Dockerfile.api", "**/Dockerfile*", false},
		{".env.local", "", false},
		{".github/workflows/x.yml", "", false},
		{"api/internal/research/x.go", "", false},
		{"console/src/pages/C5Page.tsx", "", false},
	} {
		if got := LandingHardCoreEntry("bifrost-platform", c.path); got != c.entry {
			t.Errorf("%s: entry %q, want %q", c.path, got, c.entry)
		}
		if got := LandingDecisionChain("bifrost-platform", c.path); got != c.chain {
			t.Errorf("%s: decision chain %v, want %v", c.path, got, c.chain)
		}
	}
	if LandingHardCoreEntry("bifrost-trade-infra", "api/internal/approvals/x.go") != "" {
		t.Error("another repository is absolute, never core")
	}
}
