//bifrost:owner verification

package controller_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

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
	fullMid   = "5555555555555555555555555555555555555555"
	fullOld   = "6666666666666666666666666666666666666666"
)

// fullCI is a cluster of full runs and a main, in memory.
type fullCI struct {
	main    string
	stg     string
	runs    []landingci.FullRun
	ensured []string
	now     time.Time
	// failedTasks are a run's failed pipeline tasks; modules a landed
	// commit's CI run and its run-modules (a commit not there has no run).
	failedTasks map[string][]string
	modules     map[string][2]string
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
				{Commit: fullMid, Subject: "fix: an earlier landing"}, {Commit: fullOld, Subject: "chore: the first landing"}}, nil
		},
		FailedTasks: func(_ context.Context, run string) ([]string, error) { return f.failedTasks[run], nil },
		RunModules: func(_ context.Context, revision string) (string, string, error) {
			m := f.modules[revision]
			return m[0], m[1], nil
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

// fullCIOrigin, for the tests below: 2026-11-14: the daily full CI was the
// only run that would catch a module wrongly left out of a landing's CI, and
// a red one reached nobody: a log line, a note, and a goal for a dispatch
// lane that was being removed; it also named only Go tests.

// missions counts every mission: a red full run creates none.
func missions(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.mission`).Scan(&n))
	return n
}

// redNotes reads the notes of red run run: their texts and full_ci bodies.
func redNotes(t *testing.T, pool *pgxpool.Pool, run string) (texts []string, bodies []map[string]any) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT body_json->>'text', body_json->'full_ci' FROM agentrt.event WHERE event_stream_id = 'controller'
	  AND event_type = 'note' AND body_json->'full_ci'->>'run' = $1 ORDER BY seq`, run)
	must(t, err)
	defer rows.Close()
	for rows.Next() {
		var text string
		var body map[string]any
		must(t, rows.Scan(&text, &body))
		texts, bodies = append(texts, text), append(bodies, body)
	}
	must(t, rows.Err())
	return texts, bodies
}

// Origin: fullCIOrigin. A red run is noted once, with the pipeline tasks
// that failed (Go or not) and, of the three commits landed since the last
// green full run, the one whose own CI run ran only some modules: the one
// that ran all of them ("*") is no suspect, and the one whose CI run cannot
// be found is counted as unread. No goal or mission is created, and a
// second pass adds no second note.
func TestFullCIRedIsNotedOnceWithFailedTasksAndSuspects(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "08:00")
	const red = "ci-full-111111111111-20261106"
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: "ci-full-444444444444-20261105", Revision: fullGreen, Status: "Succeeded", CreatedAt: now.Add(-29 * time.Hour)},
		{Name: red, Revision: fullMain, Status: "Failed", CreatedAt: now.Add(-5 * time.Hour),
			HasFailedTests: true, FailedTests: []string{"TestB", "TestA"}},
	},
		failedTasks: map[string][]string{red: {"task-one", "task-two"}},
		modules:     map[string][2]string{fullMain: {"ci-latest", "*"}, fullMid: {"ci-earlier", " api repo "}},
	}
	c := f.controller(pool)
	before := missions(t, pool)
	if n := pass(t, c); n != 1 {
		t.Fatal("noted", n)
	}
	if n := pass(t, c); n != 0 {
		t.Fatal("a second pass noted again", n)
	}
	if after := missions(t, pool); after != before {
		t.Fatal("a red run created a mission", before, after)
	}
	texts, bodies := redNotes(t, pool, red)
	if len(bodies) != 1 {
		t.Fatal("notes of the red run:", len(bodies))
	}
	b := bodies[0]
	want := map[string]any{"alert": true, "run": red, "commit": fullMain, "failed_key": "TestA TestB",
		"failed_tasks": []any{"task-one", "task-two"}, "unread": float64(1),
		"suspects": []any{map[string]any{"commit": fullMid, "subject": "fix: an earlier landing", "run": "ci-earlier", "modules": " api repo "}}}
	if !reflect.DeepEqual(b, want) {
		t.Fatalf("the note's body:\n%v\nwant\n%v", b, want)
	}
	for _, part := range []string{red, fullMain, "failed tasks: task-one, task-two", "failed Go tests: TestA, TestB",
		"1 commit(s) landed since the last green full run ci-full-444444444444-20261105 with a module left out of their own CI run: 55555555 ( api repo )",
		"the CI run of 1 more could not be read"} {
		if !strings.Contains(texts[0], part) {
			t.Fatalf("the note's text lacks %q:\n%s", part, texts[0])
		}
	}
}

// Origin: fullCIOrigin. Without a green full run before the red one there
// is no range of landings to look at: the note says the reconciliation was
// not done and why, and carries no suspect list that would read as "none".
// Without a reader of the failed tasks, the text says none could be read.
func TestFullCIRedWithoutAGreenIsUnreconciled(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "02:00") // before the day's runs: only the reds count
	const red = "ci-full-111111111111-20261105"
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: red, Revision: fullMain, Status: "Failed", CreatedAt: now.Add(-29 * time.Hour), HasFailedTests: true},
	}, modules: map[string][2]string{fullMid: {"ci-earlier", " api repo "}}}
	c := f.controller(pool)
	c.FullCI.FailedTasks = nil
	if n := pass(t, c); n != 1 {
		t.Fatal("noted", n)
	}
	texts, bodies := redNotes(t, pool, red)
	if len(bodies) != 1 {
		t.Fatal("notes of the red run:", len(bodies))
	}
	b := bodies[0]
	if b["unreconciled"] != "no full run in the last 14 days was green" || b["suspects"] != nil || b["unread"] != nil || b["failed_tasks"] != nil {
		t.Fatalf("the note's body: %v", b)
	}
	if !strings.Contains(texts[0], "no failed task could be read") || !strings.Contains(texts[0], "was not reconciled: no full run in the last 14 days was green") {
		t.Fatal(texts[0])
	}
	// The landings that cannot be read are a reason as well, not "none".
	f.runs = append([]landingci.FullRun{{Name: "ci-full-444444444444-20261104", Revision: fullGreen, Status: "Succeeded", CreatedAt: now.Add(-50 * time.Hour)}},
		append(f.runs, landingci.FullRun{Name: "ci-full-222222222222-20261106", Revision: fullMain2, Status: "Failed", CreatedAt: now.Add(-time.Hour)})...)
	c.FullCI.Landings = func(context.Context, string, string, int) ([]controller.FullCILanding, error) {
		return nil, errors.New("gitea is away")
	}
	pass(t, c)
	_, bodies = redNotes(t, pool, "ci-full-222222222222-20261106")
	if len(bodies) != 1 || bodies[0]["suspects"] != nil ||
		bodies[0]["unreconciled"] != "the landings since ci-full-444444444444-20261104 could not be read: gitea is away" {
		t.Fatalf("landings that could not be read: %v", bodies)
	}
}

// Origin: fullCIOrigin. The gauge the alert rules read is the newest
// finished full run: a red after a green is red, and a newer run still
// running does not replace the last finished one. No loop, no answer.
func TestFullCILatestIsTheNewestFinishedRun(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "02:00")
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: "ci-full-444444444444-20261104", Revision: fullGreen, Status: "Succeeded", CreatedAt: now.Add(-50 * time.Hour)},
	}}
	c := f.controller(pool)
	if _, _, ok := c.FullCI.Latest(); ok {
		t.Fatal("an answer before any pass")
	}
	pass(t, c)
	if name, status, ok := c.FullCI.Latest(); !ok || name != "ci-full-444444444444-20261104" || status != "Succeeded" {
		t.Fatal(name, status, ok)
	}
	f.runs = append(f.runs, landingci.FullRun{Name: "ci-full-111111111111-20261105", Revision: fullMain, Status: "Failed", CreatedAt: now.Add(-26 * time.Hour)})
	pass(t, c)
	if name, status, ok := c.FullCI.Latest(); !ok || name != "ci-full-111111111111-20261105" || status != "Failed" {
		t.Fatal("green then red", name, status, ok)
	}
	f.runs = append(f.runs, landingci.FullRun{Name: "ci-full-222222222222-20261106", Revision: fullMain2, Status: "Running", CreatedAt: now.Add(-time.Hour)})
	pass(t, c)
	if name, status, ok := c.FullCI.Latest(); !ok || name != "ci-full-111111111111-20261105" || status != "Failed" {
		t.Fatal("a running run replaced the last finished one", name, status, ok)
	}
	var none *controller.FullCI
	if _, _, ok := none.Latest(); ok {
		t.Fatal("no loop gave an answer")
	}
}

// Origin: fullCIOrigin (the goal a red run proposed is gone; a green run
// was never to leave a red note or a mission, and still does not).
func TestFullCIGreenNotesNothing(t *testing.T) {
	pool := setup(t)
	now := at(t, "2026-11-06", "08:00")
	f := &fullCI{main: fullMain, now: now, runs: []landingci.FullRun{
		{Name: "ci-full-111111111111-20261106", Revision: fullMain, Status: "Succeeded", CreatedAt: now.Add(-5 * time.Hour)},
		{Name: "ci-full-333333333333-20261106", Revision: fullSTG, Status: "Running", CreatedAt: now.Add(-5 * time.Hour)},
	}}
	before := missions(t, pool)
	n := pass(t, f.controller(pool))
	var alerts int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_stream_id = 'controller' AND body_json->'full_ci' ? 'alert'`).Scan(&alerts))
	if n != 0 || alerts != 0 || missions(t, pool) != before {
		t.Fatal("a green run was noted as red", n, alerts)
	}
}

// Origin: R5:5.7-9 (a run without the failed-tests result is read from its
// test log); 2026-11-14 (fullCIOrigin): the tests are now named in the red
// run's note, not in a goal.
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
		t.Fatal("noted", n)
	}
	texts, bodies := redNotes(t, pool, "ci-full-111111111111-20261106")
	if len(bodies) != 1 || bodies[0]["failed_key"] != "TestC TestC/sub" || !strings.Contains(texts[0], "failed Go tests: TestC, TestC/sub") ||
		bodies[0]["unreconciled"] != "no full run in the last 14 days was green" {
		t.Fatal(texts, bodies)
	}
}
