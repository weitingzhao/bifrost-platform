package controller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

const (
	fullMain  = "1111111111111111111111111111111111111111"
	fullMain2 = "2222222222222222222222222222222222222222"
	fullSTG   = "3333333333333333333333333333333333333333"
	fullGreen = "4444444444444444444444444444444444444444"
)

// fullCI is a cluster of full runs and a main, in memory.
type fullCI struct {
	main    string
	stg     string
	runs    []landingci.FullRun
	ensured []string
	now     time.Time
}

func (f *fullCI) controller(pool *pgxpool.Pool) *controller.Controller {
	c := &controller.Controller{DB: pool, Now: func() time.Time { return f.now }}
	c.Sources.Deployed = func(_ context.Context, env string) (string, map[string]any, bool) {
		if env != "stg" || f.stg == "" {
			return "", nil, false
		}
		return f.stg, nil, true
	}
	c.FullCI = &controller.FullCI{
		Runs: func(_ context.Context, since time.Time) ([]landingci.FullRun, error) {
			var out []landingci.FullRun
			for _, r := range f.runs {
				if !r.CreatedAt.Before(since) {
					out = append(out, r)
				}
			}
			return out, nil
		},
		Ensure: func(_ context.Context, revision string, day time.Time) (string, error) {
			name := landingci.FullRunName(revision, day)
			for _, r := range f.runs {
				if r.Name == name {
					return name, nil
				}
			}
			f.ensured = append(f.ensured, name)
			f.runs = append(f.runs, landingci.FullRun{Name: name, Revision: revision, Status: "Running", CreatedAt: f.now})
			return name, nil
		},
		Main: func(context.Context) (string, error) { return f.main, nil },
		Landings: func(_ context.Context, from, to string, max int) ([]controller.FullCILanding, error) {
			if from != fullGreen || to != fullMain || max <= controller.FullCILandingsMax {
				return nil, nil
			}
			return []controller.FullCILanding{{Commit: fullMain, Subject: "feat: the latest landing"},
				{Commit: "5555555555555555555555555555555555555555", Subject: "fix: an earlier landing"}}, nil
		},
	}
	return c
}

// at is a time of the day in the controller's zone.
func at(t *testing.T, day, hhmm string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation(store.BriefZone)
	must(t, err)
	v, err := time.ParseInLocation("2006-01-02 15:04", day+" "+hhmm, loc)
	must(t, err)
	return v
}

func pass(t *testing.T, c *controller.Controller) int {
	t.Helper()
	n, err := c.FullCIPass(ctx)
	must(t, err)
	return n
}

func TestFullCIOneRunPerCommitPerDay(t *testing.T) {
	pool := setup(t)
	f := &fullCI{main: fullMain, now: at(t, "2026-11-06", "02:59")}
	c := f.controller(pool)
	if pass(t, c); len(f.ensured) != 0 {
		t.Fatal("a run started before its time", f.ensured)
	}
	f.now = at(t, "2026-11-06", "03:00")
	if n := pass(t, c); n != 1 || len(f.ensured) != 1 || f.ensured[0] != "ci-full-111111111111-20261106" {
		t.Fatal(n, f.ensured)
	}
	// Later the same day, with main moved on: nothing more today.
	f.main, f.now = fullMain2, at(t, "2026-11-06", "10:00")
	pass(t, c)
	pass(t, c)
	if len(f.ensured) != 1 {
		t.Fatal("two runs on one day", f.ensured)
	}
	// A restarted controller sees today's run and starts none.
	if pass(t, f.controller(pool)); len(f.ensured) != 1 {
		t.Fatal("a restart started another run", f.ensured)
	}
	f.now = at(t, "2026-11-07", "03:01")
	if pass(t, c); len(f.ensured) != 2 || f.ensured[1] != "ci-full-222222222222-20261107" {
		t.Fatal("the next day", f.ensured)
	}
}

func TestFullCIRunsSTGCommitWhenItDiffers(t *testing.T) {
	pool := setup(t)
	f := &fullCI{main: fullMain, stg: fullSTG, now: at(t, "2026-11-06", "03:00")}
	c := f.controller(pool)
	if n := pass(t, c); n != 2 || len(f.ensured) != 2 || f.ensured[1] != "ci-full-333333333333-20261106" {
		t.Fatal(n, f.ensured)
	}
	// STG runs main's head: one run of that commit.
	f.stg, f.now = fullMain, at(t, "2026-11-07", "03:00")
	if pass(t, c); len(f.ensured) != 3 || f.ensured[2] != "ci-full-111111111111-20261107" {
		t.Fatal(f.ensured)
	}
	var started int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'controller' AND body_json->'full_ci' ? 'started'`).Scan(&started))
	if started != 2 {
		t.Fatal("the starts the Console shows:", started)
	}
}

func TestFullCISwitchOffAndParam(t *testing.T) {
	env := func(vars map[string]string) func(string) string { return func(k string) string { return vars[k] } }
	on, at, param, err := controller.FullCISettings(env(nil))
	if !on || at != "03:00" || param || err != nil {
		t.Fatal("defaults", on, at, param, err)
	}
	if on, _, _, _ = controller.FullCISettings(env(map[string]string{"AGENTRT_FULL_CI": "off"})); on {
		t.Fatal("AGENTRT_FULL_CI=off left the loop on")
	}
	if _, _, param, _ = controller.FullCISettings(env(map[string]string{"AGENTRT_FULL_CI_PARAM": "on"})); !param {
		t.Fatal("AGENTRT_FULL_CI_PARAM=on did not add the param")
	}
	if _, _, param, _ = controller.FullCISettings(env(map[string]string{"AGENTRT_FULL_CI_PARAM": "yes"})); param {
		t.Fatal("the param without on")
	}
	if on, at, _, _ = controller.FullCISettings(env(map[string]string{"AGENTRT_FULL_CI_AT": "05:30"})); !on || at != "05:30" {
		t.Fatal(on, at)
	}
	if on, _, _, err = controller.FullCISettings(env(map[string]string{"AGENTRT_FULL_CI_AT": "25:00"})); on || err == nil {
		t.Fatal("a time that does not parse", on, err)
	}
	// Off, the wiring leaves FullCI nil: a pass starts nothing.
	c := &controller.Controller{}
	if n, err := c.FullCIPass(ctx); n != 0 || err != nil {
		t.Fatal(n, err)
	}
}

func fullGoals(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT goal FROM agentrt.mission WHERE title LIKE 'Fix the red full CI run %' ORDER BY mission_id`)
	must(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var g string
		must(t, rows.Scan(&g))
		out = append(out, g)
	}
	must(t, rows.Err())
	return out
}

func TestFullCIRedProposesOneGoalListingTheLandings(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "08:00")
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: "ci-full-444444444444-20261105", Revision: fullGreen, Status: "Succeeded", CreatedAt: now.Add(-29 * time.Hour)},
		{Name: "ci-full-111111111111-20261106", Revision: fullMain, Status: "Failed", CreatedAt: now.Add(-5 * time.Hour),
			HasFailedTests: true, FailedTests: []string{"TestB", "TestA"}},
	}}
	c := f.controller(pool)
	if n := pass(t, c); n != 1 {
		t.Fatal("proposed", n)
	}
	pass(t, c)
	goals := fullGoals(t, pool)
	if len(goals) != 1 {
		t.Fatal("goals", len(goals))
	}
	for _, want := range []string{"ci-full-111111111111-20261106", fullMain, "- TestA\n- TestB\n", "ci-full-444444444444-20261105 of " + fullGreen,
		"- " + fullMain + " feat: the latest landing\n", "fix: an earlier landing", "Reproduce the failing tests at main's head", "bisect", "Fix forward, with a test"} {
		if !strings.Contains(goals[0], want) {
			t.Fatalf("the goal lacks %q:\n%s", want, goals[0])
		}
	}
	var noted int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'controller' AND event_type = 'note'
	  AND body_json->'full_ci'->>'run' = 'ci-full-111111111111-20261106' AND body_json->'full_ci' ? 'mission'
	  AND body_json->'full_ci'->>'alert' = 'true' AND body_json->'full_ci'->>'deviation' = 'content_reuse'`).Scan(&noted))
	if noted != 1 {
		t.Fatal("the note the Console shows:", noted)
	}
}

func TestFullCISameFailuresProposeNoneWhileTheGoalIsOpen(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "02:00") // before the day's runs: only the reds count
	red := func(name, commit string, ago time.Duration, tests ...string) landingci.FullRun {
		return landingci.FullRun{Name: name, Revision: commit, Status: "Failed", CreatedAt: now.Add(-ago), HasFailedTests: true, FailedTests: tests}
	}
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{red("ci-full-111111111111-20261105", fullMain, 29*time.Hour, "TestA")}}
	c := f.controller(pool)
	if n := pass(t, c); n != 1 {
		t.Fatal("the first red proposed", n)
	}
	f.runs = append(f.runs, red("ci-full-222222222222-20261106", fullMain2, 5*time.Hour, "TestA"))
	if n := pass(t, c); n != 0 || len(fullGoals(t, pool)) != 1 {
		t.Fatal("a second red with the same failures proposed", n)
	}
	var open string
	must(t, pool.QueryRow(ctx, `SELECT body_json->'full_ci'->>'open_run' FROM agentrt.event WHERE event_stream_id = 'controller'
	  AND body_json->'full_ci'->>'run' = 'ci-full-222222222222-20261106'`).Scan(&open))
	if open != "ci-full-111111111111-20261105" {
		t.Fatal("the note names", open)
	}
	// Other failures are another goal.
	f.runs = append(f.runs, red("ci-full-333333333333-20261106", fullSTG, 4*time.Hour, "TestB"))
	if n := pass(t, c); n != 1 {
		t.Fatal("other failures proposed", n)
	}
	// Once the first goal is no longer open, the same failures propose again.
	var first string
	must(t, pool.QueryRow(ctx, `SELECT mission_id FROM agentrt.mission WHERE title = 'Fix the red full CI run ci-full-111111111111-20261105'`).Scan(&first))
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", first, "stopped", "test") }))
	f.runs = append(f.runs, red("ci-full-111111111111-20261106", fullMain, 3*time.Hour, "TestA"))
	if n := pass(t, c); n != 1 || len(fullGoals(t, pool)) != 3 {
		t.Fatal("after the goal closed", n)
	}
}

func TestFullCIGreenProposesNothing(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "08:00")
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: "ci-full-111111111111-20261106", Revision: fullMain, Status: "Succeeded", CreatedAt: now.Add(-5 * time.Hour)},
		{Name: "ci-full-333333333333-20261106", Revision: fullSTG, Status: "Running", CreatedAt: now.Add(-5 * time.Hour)},
	}}
	if n := pass(t, f.controller(pool)); n != 0 || len(fullGoals(t, pool)) != 0 {
		t.Fatal("a green run proposed", n)
	}
}

func TestFullCIRedReadsTheTestLogWithoutTheResult(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "02:00")
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: "ci-full-111111111111-20261106", Revision: fullMain, Status: "Failed", CreatedAt: now.Add(-time.Hour)},
	}}
	c := f.controller(pool)
	c.FullCI.TestLog = func(_ context.Context, run string) (string, error) {
		return "=== RUN   TestC\n    --- FAIL: TestC/sub (0.01s)\n--- FAIL: TestC (0.02s)\nFAIL\n", nil
	}
	if n := pass(t, c); n != 1 {
		t.Fatal("proposed", n)
	}
	if g := fullGoals(t, pool); len(g) != 1 || !strings.Contains(g[0], "(from its test-api log):\n- TestC\n- TestC/sub\n") ||
		!strings.Contains(g[0], "No full run in the last 14 days was green") {
		t.Fatal(g)
	}
}
