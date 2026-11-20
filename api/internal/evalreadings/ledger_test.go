package evalreadings

import (
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// median must equal Postgres percentile_cont(0.5), which the first live check
// (2026-11-20, against the PROD ledger) used: the mean of the two middle values.
func TestMedianIsPercentileCont(t *testing.T) {
	if v := median([]float64{3, 1, 2}); *v != 2 {
		t.Fatalf("odd: %v", *v)
	}
	if v := median([]float64{4, 1, 3, 2}); *v != 2.5 {
		t.Fatalf("even: %v", *v)
	}
	if median(nil) != nil {
		t.Fatal("empty must be nil")
	}
}

func row(typ string, at time.Time, body map[string]any) eventlog.Row {
	return eventlog.Row{Type: typ, At: at, Body: body}
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// The readings from rows the ledger would give: runs inside and outside the
// window, an approval decided before the window, a deploy whose commit never
// reached main in the lookback, a stg deploy.
func TestLedgerReadingsFromRows(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), 7)
	mid := w.From.Add(48 * time.Hour)
	var pipes []eventlog.Row
	for i := 0; i < 12; i++ {
		start := mid.Add(time.Duration(i) * time.Minute)
		result := "Succeeded"
		if i == 0 {
			result = "Failed"
		}
		pipes = append(pipes, row("pipeline_seen", start, map[string]any{"uid": i, "kind": "ci", "pipeline": "bifrost-ci-platform", "result": result,
			"created_at": ts(start.Add(-10 * time.Second)), "started_at": ts(start), "completed_at": ts(start.Add(time.Duration(100+i) * time.Second))}))
	}
	// Completed after the window closed: not counted.
	late := w.To.Add(time.Hour)
	pipes = append(pipes, row("pipeline_seen", late, map[string]any{"kind": "ci", "pipeline": "bifrost-ci-platform", "result": "Failed",
		"created_at": ts(late), "started_at": ts(late), "completed_at": ts(late.Add(time.Minute))}))
	pipes = append(pipes, row("pipeline_seen", mid, map[string]any{"kind": "delivery", "pipeline": "bifrost-deliver-platform", "result": "Succeeded",
		"created_at": ts(mid), "started_at": ts(mid), "completed_at": ts(mid.Add(time.Minute))}))

	approvals := []eventlog.Row{
		row("approval_seen", mid, map[string]any{"approval": 1, "created_at": ts(mid), "decided_at": ts(mid.Add(60 * time.Second))}),
		row("approval_seen", mid, map[string]any{"approval": 2, "created_at": ts(mid), "decided_at": ts(mid.Add(120 * time.Second))}),
		row("approval_seen", mid, map[string]any{"approval": 3, "created_at": ts(w.From.Add(-time.Hour)), "decided_at": ts(w.From.Add(-time.Minute))}),
		row("approval_seen", mid, map[string]any{"approval": 4, "created_at": ts(mid)}), // still pending
	}
	questions := []eventlog.Row{row("question_seen", mid, map[string]any{"question": "q1", "answered_at": ts(mid)})}
	deploys := []eventlog.Row{
		row("release.deploy.finished", mid.Add(10*time.Minute), map[string]any{"env": "prod", "change": "succeeded", "repo": "bifrost-platform", "sha": "aaa"}),
		row("release.deploy.finished", mid.Add(20*time.Minute), map[string]any{"env": "prod", "change": "succeeded", "repo": "bifrost-platform", "sha": "aaa"}),
		row("release.deploy.finished", mid.Add(30*time.Minute), map[string]any{"env": "prod", "change": "succeeded", "repo": "bifrost-platform", "sha": "zzz"}),
		row("release.deploy.finished", mid, map[string]any{"env": "stg", "change": "succeeded", "repo": "bifrost-platform", "sha": "aaa"}),
		row("release.deploy.finished", mid, map[string]any{"env": "prod", "change": "failed", "repo": "bifrost-platform", "sha": "bbb"}),
	}
	mains := []eventlog.Row{row("git.main.advanced", mid, map[string]any{"repo": "bifrost-platform", "commit": "aaa"})}

	src := ledgerSource{
		latestByKey: func(typ, _ string, _ time.Time) ([]eventlog.Row, error) {
			return map[string][]eventlog.Row{"pipeline_seen": pipes, "approval_seen": approvals, "question_seen": questions}[typ], nil
		},
		ofType: func(typ string, _, _ time.Time) ([]eventlog.Row, error) {
			return map[string][]eventlog.Row{"release.deploy.finished": deploys, "git.main.advanced": mains}[typ], nil
		},
	}
	rs, err := src.readings(w)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Reading{}
	for _, r := range rs {
		got[r.Name] = r
	}
	check := func(name string, want float64, samples int) {
		t.Helper()
		r, ok := got[name]
		if !ok || r.Value == nil || *r.Value != want || r.Samples != samples || r.Status != StatusOK {
			t.Errorf("%s = %+v, want %v over %d", name, r, want, samples)
		}
	}
	check("ci_run_p50_seconds", 105.5, 12) // 100..111, median of 12 values
	// 2026-11-20, the CI line: queue is created_at -> first_step_at; the runs
	// here carry none, so the queue is unmeasured, never 0 (version 1 read 0
	// seconds on PROD). TestQueueFromFirstStep is its positive control.
	if r := got["ci_queue_p50_seconds"]; r.Status != StatusUnmeasured || r.Value != nil || r.Definition.Version != "2" {
		t.Errorf("queue without first_step_at: %+v", r)
	}
	check("ci_success_ratio", 0.917, 12) // 11/12 rounded to 3 places
	check("delivery_success_ratio", 1, 1)
	check("owner_actions", 3, 3) // approvals 1, 2 and question q1; 3 is before the window, 4 pending
	if r := got["owner_wait_p50_seconds"]; r.Status != StatusTooFew || r.Samples != 2 {
		t.Errorf("owner wait with 2 samples must be too_few: %+v", r)
	}
	check("prod_deploys", 3, 3) // the stg and the failed one are not counted
	if r := got["main_to_prod_p50_seconds"]; r.Status != StatusTooFew || r.Samples != 1 {
		// aaa paired at its first prod deploy (10 minutes); zzz never reached main in the lookback.
		t.Errorf("main to prod: %+v", r)
	}
	if got["prod_deploys"].Labels["bifrost-platform"] != "3" {
		t.Errorf("per-repo label: %+v", got["prod_deploys"].Labels)
	}
}

// Positive control for the queue without first_step_at above: runs that carry
// it give created_at -> first_step_at (2026-11-20, the CI line's definition).
func TestQueueFromFirstStep(t *testing.T) {
	w := ClosedWindow(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), 7)
	mid := w.From.Add(48 * time.Hour)
	var rows []eventlog.Row
	for i := 0; i < 11; i++ {
		c := mid.Add(time.Duration(i) * time.Minute)
		b := map[string]any{"kind": "ci", "pipeline": "bifrost-ci-infra", "result": "Succeeded",
			"created_at": ts(c), "started_at": ts(c), "completed_at": ts(c.Add(5 * time.Minute))}
		if i < 10 {
			b["first_step_at"] = ts(c.Add(time.Duration(20+i) * time.Second))
		}
		rows = append(rows, row("pipeline_seen", c, b))
	}
	var q Reading
	for _, r := range ciReadings(rows, w) {
		if r.Name == "ci_queue_p50_seconds" {
			q = r
		}
	}
	if q.Status != StatusOK || q.Samples != 10 || q.Value == nil || *q.Value != 24.5 {
		t.Fatalf("queue from first_step_at: %+v", q)
	}
}
