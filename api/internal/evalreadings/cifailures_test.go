package evalreadings

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// 2026-11-20: the briefing read a fall in CI success (94.3% to 90.5%) as CI
// getting worse; the CI line traced 15 of 19 failed ci-* runs to guard tests
// stopping a commit (test-api, code-health) and 3 to ci-infra itself. The
// evidence ledger already names the failed tasks (statement.predicate.failed_tasks).
func TestCIFailuresByFailedTask(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 1)
	at := w.From.Add(time.Hour).Format(time.RFC3339)
	run := func(name, pipeline, result string) eventlog.Row {
		return eventlog.Row{Body: map[string]any{"kind": "ci", "run": name, "pipeline": pipeline, "result": result,
			"repo": "bifrost-platform", "revision": "c-" + name, "started_at": at, "completed_at": at}}
	}
	rows := []eventlog.Row{
		run("ci-api-a", "ci-platform", "Failed"),
		run("ci-api-b", "ci-platform", "Failed"),
		run("ci-api-c", "ci-platform", "Succeeded"),
		run("land-x", "land", "Failed"),
	}
	// What the server's lookup returns per run (land runs write no evidence row).
	ledger := map[string][]string{
		"c-ci-api-a/ci-api-a": {"test-api"},
		"c-ci-api-b/ci-api-b": {"test-api", "code-health"},
	}
	look := func(repo, commit, run string) ([]string, error) { return ledger[commit+"/"+run], nil }
	got := map[string]float64{}
	for _, r := range ciFailureReadings(rows, look, w) {
		got[r.Labels["pipeline"]+"/"+r.Labels["failed_task"]] = *r.Value
	}
	want := map[string]float64{
		"ci-platform/test-api":    2,
		"ci-platform/code-health": 1,
		"land/unrecorded":         1, // land runs write no evidence row
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: got %v, want %v (all: %v)", k, got[k], v, got)
		}
	}
}

// Positive control for the case above: no failed run in the window is a
// measured zero, not unmeasured.
func TestCIFailuresNoneIsZero(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 1)
	rs := ciFailureReadings(nil, nil, w)
	if len(rs) != 1 || rs[0].Status != StatusOK || rs[0].Value == nil || *rs[0].Value != 0 {
		t.Fatalf("got %+v", rs)
	}
}
