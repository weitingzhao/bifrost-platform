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
	check("ci_success_ratio", 0.917, 12)   // 11/12 rounded to 3 places
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

// 2026-11-20, the metrics line: how often the Owner's phone was pushed about a
// card had no system record across "relax cards into records" (law 46); the
// comm line put each push on the ledger as approval.card.notified from the
// PROD release of 16:20:43Z. Shape from PROD: kind created, one kind failed
// (#662, 17:22:42Z). A window starting before the ledger kept them is
// unmeasured; a later window with no push is a measured 0.
func TestOwnerInterruptions(t *testing.T) {
	src := func(rows []eventlog.Row) ledgerSource {
		return ledgerSource{ofType: func(typ string, _, _ time.Time) ([]eventlog.Row, error) {
			if typ != "approval.card.notified" {
				t.Fatalf("read %s", typ)
			}
			return rows, nil
		}}
	}
	day := ClosedWindow(time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC), 1)
	at := day.From.Add(time.Hour)
	rows := []eventlog.Row{
		row("approval.card.notified", at, map[string]any{"kind": "created", "channel": "ntfy"}),
		row("approval.card.notified", at, map[string]any{"kind": "failed", "channel": "ntfy"}),
		row("approval.card.notified", at, map[string]any{"kind": "created", "channel": "ntfy"}),
		row("approval.card.notified", at, map[string]any{"kind": "reminder_waiting", "channel": "ntfy"}),
	}
	r, err := src(rows).interruptionReading(day)
	if err != nil || r.Status != StatusOK || *r.Value != 3 || r.Labels["failed"] != "1" {
		t.Fatalf("three pushes and one failure: %+v %v", r, err)
	}
	r, _ = src(nil).interruptionReading(day)
	if r.Status != StatusOK || *r.Value != 0 {
		t.Fatalf("no push is a measured 0: %+v", r)
	}
	r, _ = src(rows).interruptionReading(ClosedWindow(time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), 1))
	if r.Status != StatusUnmeasured || r.Value != nil {
		t.Fatalf("11-20 starts before the ledger kept pushes: %+v", r)
	}
}
