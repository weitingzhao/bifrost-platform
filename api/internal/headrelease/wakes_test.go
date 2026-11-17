package headrelease

import "testing"

// 2026-11-17 00:11, PROD: head-release kept ~400 LISTs per 10 minutes
// because any pipeline's finished run and every window renewal woke it.
func TestWakesAreTheRowsOwn(t *testing.T) {
	w := WakesOf(Table{Deliverables: []Deliverable{
		{Name: "platform-stg", Enabled: true, Pipeline: "bifrost-deliver-platform", Commits: []Commit{
			{Repo: "bifrost-platform", Param: "revision"},
			{Repo: "bifrost-ui", Param: "uiRevision", TestedWith: "bifrost-platform"},
		}},
		{Name: "platform-prod", Enabled: false, Pipeline: "bifrost-deliver-platform-prod"},
	}})
	for _, c := range []struct {
		typ, subject, change string
		want                 bool
	}{
		{"git.main.advanced", "bifrost-platform@abc", "", true},
		{"git.main.advanced", "bifrost-research@abc", "", false},
		{"ci.run.finished", "ci-platform-x9@abc", "", true},
		{"ci.run.finished", "bifrost-deliver-platform-1794@abc", "", true},
		{"ci.run.finished", "ci-research-x@abc", "", false},
		{"ci.run.finished", "land-abc@abc", "", false},
		{"release.window.changed", "window", "released", true},
		{"release.window.changed", "window", "expired", true},
		{"release.window.changed", "window", "held", false},
		{"release.window.changed", "window", "queued", false},
	} {
		if got := w.Worth(c.typ, c.subject, c.change); got != c.want {
			t.Errorf("%s %s %s: %v, want %v", c.typ, c.subject, c.change, got, c.want)
		}
	}
}
