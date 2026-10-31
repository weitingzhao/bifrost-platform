package delivery

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
)

// W-31 B5: a Console/MCP-started platform CI names repo-url once, with the
// value the release reader accepts; other CI pipelines are unchanged.
func TestCIRunSpecRepoURL(t *testing.T) {
	t.Parallel()
	if platformCIRepoURL != landingci.PlatformRepoURL {
		t.Fatalf("delivery %q != landingci %q", platformCIRepoURL, landingci.PlatformRepoURL)
	}
	for _, tc := range []struct{ pipeline, repo, want string }{
		{"bifrost-ci-platform", "bifrost-platform", landingci.PlatformRepoURL},
		{"bifrost-ci-frontend", "bifrost-trade-frontend", ""},
		{"bifrost-ci-python", "bifrost-trade-core", ""},
	} {
		spec := ciRunSpec(tc.pipeline, factSHA1, tc.repo)
		var got []string
		for _, p := range spec["params"].([]map[string]any) {
			if p["name"] == "repo-url" {
				got = append(got, p["value"].(string))
			}
		}
		if tc.want == "" && len(got) != 0 || tc.want != "" && (len(got) != 1 || got[0] != tc.want) {
			t.Fatalf("%s: repo-url %v, want %q", tc.pipeline, got, tc.want)
		}
	}
	// The caller still cannot choose it.
	if _, _, err := ciRunInputs("bifrost-ci-platform", factSHA1, map[string]string{"repo-url": "http://example.invalid/x.git"}); err == nil {
		t.Fatal("caller-set repo-url accepted")
	}
}
