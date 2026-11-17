//bifrost:owner verification

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// 2026-11-14: the daily full CI was the only run that would catch a module
// wrongly left out of a landing's CI, and a red one reached nobody: a log
// line, a note, and a goal for a dispatch lane that was being removed; it
// also named only Go tests. Now the newest finished run is a gauge on
// /metrics, where the alert rules read (1 for red), and the notes are read
// by GET /units/full-ci, newest first. A platform without a controller
// (platform-api) writes no such series, and one without the database
// answers 503.
func TestFullCIGaugeAndNotesRoute(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	pool := db.Pool(t, db.Runtime)
	loc, err := time.LoadLocation(planpace.BriefZone)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 2, 0, 0, 0, loc) // before the day's runs: the pass only reads
	const green, red, last = "ci-full-444444444444-20261104", "ci-full-111111111111-20261105", "ci-full-222222222222-20261106"
	runs := []landingci.FullRun{
		{Name: green, Revision: strings.Repeat("4", 40), Status: "Succeeded", CreatedAt: now.Add(-50 * time.Hour)},
		{Name: red, Revision: strings.Repeat("1", 40), Status: "Failed", CreatedAt: now.Add(-26 * time.Hour)},
	}
	c := &controller.Controller{DB: pool, Now: func() time.Time { return now }}
	c.FullCI = &fullci.FullCI{
		Runs:   func(context.Context, time.Time) ([]landingci.FullRun, error) { return runs, nil },
		Ensure: func(context.Context, string, time.Time) (string, error) { return "", nil },
		Main:   func(context.Context) (string, error) { return strings.Repeat("2", 40), nil },
		FailedTasks: func(_ context.Context, run string) ([]string, error) {
			return []string{"task-of-" + run}, nil
		},
	}

	// No plugin probes: the scrape is about the platform's own gauges.
	srv := &Server{authLoaded: true, httpMetrics: newHTTPMetrics(), pluginProbes: []func(context.Context) probe.PluginHealth{}}
	scrape := func() string {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleMetrics(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		return rec.Body.String()
	}
	notes := func() (int, []map[string]any) {
		t.Helper()
		rec := httptest.NewRecorder()
		srv.handleUnitFullCI(rec, httptest.NewRequest(http.MethodGet, "/api/v1/units/full-ci", nil))
		var out struct {
			Runs []map[string]any `json:"runs"`
		}
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return rec.Code, out.Runs
	}

	// No controller and no database: no series, and 503.
	if body := scrape(); strings.Contains(body, "bifrost_full_ci_red") {
		t.Fatalf("a platform without a controller wrote the gauge:\n%s", body)
	}
	if code, _ := notes(); code != http.StatusServiceUnavailable {
		t.Fatal("without the database:", code)
	}
	// A controller whose loop has not passed yet, or is off: still none.
	srv.agentRuntime = &agentRuntime{api: &agentrtapi.API{DB: pool}}
	if body := scrape(); strings.Contains(body, "bifrost_full_ci_red") {
		t.Fatalf("the gauge before any pass:\n%s", body)
	}
	srv.agentRuntime.fullCI.Store(c.FullCI)
	if code, list := notes(); code != http.StatusOK || list == nil || len(list) != 0 {
		t.Fatal("before any note:", code, list)
	}

	if _, err := c.FullCIPass(ctx); err != nil {
		t.Fatal(err)
	}
	body := scrape()
	for _, want := range []string{
		"# HELP bifrost_full_ci_red Whether the newest finished daily full CI run is red (1) or green (0)\n",
		"# TYPE bifrost_full_ci_red gauge\n",
		`bifrost_full_ci_red{run="` + red + `"} 1` + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in /metrics:\n%s", want, body)
		}
	}
	// A second red, then a green: the notes come newest first, and the
	// gauge follows the newest finished run back to 0.
	runs = append(runs, landingci.FullRun{Name: last, Revision: strings.Repeat("2", 40), Status: "Failed", CreatedAt: now.Add(-2 * time.Hour)})
	if _, err := c.FullCIPass(ctx); err != nil {
		t.Fatal(err)
	}
	code, list := notes()
	if code != http.StatusOK || len(list) != 2 {
		t.Fatal(code, list)
	}
	for i, run := range []string{last, red} {
		body, _ := list[i]["full_ci"].(map[string]any)
		tasks, _ := body["failed_tasks"].([]any)
		text, _ := list[i]["text"].(string)
		at, _ := list[i]["at"].(string)
		if _, err := time.Parse(time.RFC3339, at); err != nil || body["run"] != run || len(tasks) != 1 || tasks[0] != "task-of-"+run ||
			!strings.Contains(text, run) {
			t.Fatalf("note %d is not %s's: %v", i, run, list[i])
		}
	}
	runs = append(runs, landingci.FullRun{Name: "ci-full-333333333333-20261106", Revision: strings.Repeat("3", 40), Status: "Succeeded", CreatedAt: now.Add(-time.Hour)})
	if _, err := c.FullCIPass(ctx); err != nil {
		t.Fatal(err)
	}
	if body := scrape(); !strings.Contains(body, `bifrost_full_ci_red{run="ci-full-333333333333-20261106"} 0`+"\n") {
		t.Fatalf("the gauge after a green:\n%s", body)
	}
}
