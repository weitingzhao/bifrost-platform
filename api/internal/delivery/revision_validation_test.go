package delivery

import "testing"

func TestValidateRevision(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rev   string
		valid bool
	}{
		{"main", true},
		{"feature/release-ui-polish", true},
		{"v0.2.0", true},
		{"# pasted markdown", false},
		{"line1\nline2", false},
		{string(make([]byte, 257)), false},
	}
	for _, tc := range cases {
		err := validateRevision(tc.rev)
		if tc.valid && err != nil {
			t.Errorf("validateRevision(%q) = %v, want nil", tc.rev, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("validateRevision(%q) = nil, want error", tc.rev)
		}
	}
}

func TestIntersectRefSets(t *testing.T) {
	t.Parallel()
	got := intersectRefSets([]map[string]bool{
		{"main": true, "feature/x": true},
		{"main": true, "feature/y": true},
	})
	if len(got) != 1 || got[0] != "main" {
		t.Fatalf("intersectRefSets = %v, want [main]", got)
	}
}

func TestReposForPipeline(t *testing.T) {
	t.Parallel()
	platform := reposForPipeline("bifrost-deliver-platform")
	if len(platform) != 2 {
		t.Fatalf("platform repos = %v, want 2", platform)
	}
	trade := reposForPipeline("bifrost-deliver-stg")
	if len(trade) != 7 {
		t.Fatalf("trade repos = %v, want 7", trade)
	}
	unknown := reposForPipeline("does-not-exist")
	if len(unknown) != len(trackedGiteaRepos) {
		t.Fatalf("unknown pipeline should fall back to all tracked repos")
	}
}
