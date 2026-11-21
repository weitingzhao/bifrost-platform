package delivery

import "testing"

// releaseReaderRepoURL is the repo-url the release reader (verification's
// landingci) accepts for platform CI: the value is the contract, pinned the
// same way in landingci's own test, so the two sides cannot drift apart without
// one of them failing (2026-11-21, tests reaching across domains go to zero).
const releaseReaderRepoURL = "http://gitea.cicd.svc.cluster.local:3000/bifrost/bifrost-platform.git"

// W-31 B5: a Console/MCP-started platform CI names repo-url once, with the
// value the release reader accepts; other CI pipelines are unchanged.
func TestCIRunSpecRepoURL(t *testing.T) {
	t.Parallel()
	if platformCIRepoURL != releaseReaderRepoURL {
		t.Fatalf("delivery %q != the release reader's %q", platformCIRepoURL, releaseReaderRepoURL)
	}
	for _, tc := range []struct{ pipeline, repo, want string }{
		{"bifrost-ci-platform", "bifrost-platform", releaseReaderRepoURL},
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
