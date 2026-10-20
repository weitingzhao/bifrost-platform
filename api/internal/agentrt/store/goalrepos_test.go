package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// A goal names its repositories sorted, at least one, at most GoalMaxRepos,
// each known, none twice.
func TestCheckGoalRepos(t *testing.T) {
	t.Parallel()
	if got, err := store.CheckGoalRepos([]string{infra, "bifrost-platform"}); err != nil || !slices.Equal(got, []string{"bifrost-platform", infra}) {
		t.Fatalf("two repositories: %v %v", got, err)
	}
	for name, c := range map[string]struct {
		repos []string
		why   string
	}{
		"none":     {nil, "may name bifrost-platform, bifrost-trade-infra, not none"},
		"empty":    {[]string{}, "not none"},
		"unknown":  {[]string{"bifrost-platform", "bifrost-trade-api"}, `not "bifrost-trade-api"`},
		"twice":    {[]string{infra, "bifrost-platform", infra}, "names bifrost-trade-infra twice"},
		"too many": {[]string{"a", "b", "c", "d", "e", "f"}, "at most 5 repositories, not 6"},
	} {
		if _, err := store.CheckGoalRepos(c.repos); !errors.Is(err, store.ErrInvalid) || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
}

// twoRepoPlan puts a task in each repository of a two-repository goal, the
// platform's with a check of its own, after infra's.
const twoRepoPlan = "Two changes.\n\n```json\n" + `{
  "mission": {"success": ["both land"]},
  "tasks": [{"ref": "t1", "title": "Test the probe", "repo": "bifrost-trade-infra", "text": "Add a test to scripts/agentrt.", "verify": []},
    {"ref": "t2", "title": "Name the test", "repo": "bifrost-platform", "text": "Name it in repos.go.",
     "verify": [["go", "-C", "api", "test", "./internal/agentrt/store/"]], "depends_on": ["t1"]}]
}` + "\n```\n"

// A goal of two repositories (the Owner 10-19, T26): main is read for each;
// the mission keeps both bases; the planning task has the first repository
// checked out at its base and the other beside it as a sibling at main, and
// its text says which checks each takes; the plan's tasks are made in their
// own repositories, each from its own base, with its own checks. The same
// goal keyed with repo or with repos of one is one request.
func TestGoalOfTwoRepos(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	var read []string
	main := func(_ context.Context, repo string) (string, error) {
		read = append(read, repo)
		if repo == infra {
			return commitB, nil
		}
		return commitA, nil
	}
	out, err := store.ProposeGoal(ctx, w.rt, owner, "g1", store.Goal{Title: "Test the probe", Text: "Test the probe",
		Repos: []string{infra, "bifrost-platform"}, Main: main})
	ok(t, err)
	mission, plan := out["mission_id"].(string), out["task_id"].(string)
	repos, _ := out["repos"].([]any)
	bases, _ := out["bases"].(map[string]any)
	if out["repo"] != "bifrost-platform" || out["base_commit"] != commitA || len(repos) != 2 || repos[0] != "bifrost-platform" ||
		repos[1] != infra || bases["bifrost-platform"] != commitA || bases[infra] != commitB || !slices.Equal(read, []string{"bifrost-platform", infra}) {
		t.Fatalf("the answer: %v (main read for %v)", out, read)
	}
	var scope []byte
	var planRepo, planBase string
	ok(t, w.rt.QueryRow(ctx, `SELECT m.scope_json::text, t.repo, t.base_commit FROM agentrt.mission m
	  JOIN agentrt.task t ON t.mission_id = m.mission_id WHERE t.task_id = $1`, plan).Scan(&scope, &planRepo, &planBase))
	var sc struct {
		Repos []string
		Bases map[string]string
	}
	ok(t, json.Unmarshal(scope, &sc))
	if !slices.Equal(sc.Repos, []string{"bifrost-platform", infra}) || sc.Bases["bifrost-platform"] != commitA || sc.Bases[infra] != commitB ||
		planRepo != "bifrost-platform" || planBase != commitA {
		t.Fatalf("the mission's scope %s; the plan on %s at %s", scope, planRepo, planBase)
	}
	var body []byte
	ok(t, w.rt.QueryRow(ctx, `SELECT b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.task_id = $1`, plan).Scan(&body))
	var spec struct {
		Text     string
		Siblings []struct{ Repo, Ref string }
	}
	ok(t, json.Unmarshal(body, &spec))
	if len(spec.Siblings) != 1 || spec.Siblings[0].Repo != infra || spec.Siblings[0].Ref != "main" {
		t.Fatalf("the planning spec's siblings: %+v", spec.Siblings)
	}
	for _, want := range []string{"repositories bifrost-platform, bifrost-trade-infra",
		"bifrost-platform is checked out here at " + commitA, "bifrost-trade-infra is beside it, at ../bifrost-trade-infra (the head of its main; its tasks start from " + commitB,
		"one task in each, ordered with depends_on", "  - bifrost-platform: a test of the package you change", "  - bifrost-trade-infra: none: leave verify empty.",
		// What each task works in (T26's first run): its own checkout and nothing else.
		"  - a task in bifrost-platform has bifrost-platform, with ../bifrost-ui at the head of its main beside it.\n",
		"  - a task in bifrost-trade-infra has bifrost-trade-infra alone.\n", "The repositories beside you now are for your reading only; no task has them.",
		"is a task in that repository", "never from another task's work", "names ../<repository> its checkout does not have is refused"} {
		if !strings.Contains(spec.Text, want) {
			t.Fatalf("the planning spec lacks %q:\n%s", want, spec.Text)
		}
	}
	// The same goal of one repository, as repo before and as repos now, is
	// one request under one key.
	one := store.Goal{Title: "Docs", Text: "Docs", Repos: []string{"bifrost-platform"}, Main: main}
	first, err := store.ProposeGoal(ctx, w.rt, owner, "g-one", one)
	ok(t, err)
	if again, rerr := store.ProposeGoal(ctx, w.rt, owner, "g-one", one); rerr != nil || again["task_id"] != first["task_id"] {
		t.Fatalf("a retry: %v %v", again, rerr)
	}
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		return store.MoveTask(ctx, tx, "controller", first["task_id"].(string), "cancelled", "test")
	}))

	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", mission, "active", "test") }))
	_, err = store.Dispatch(ctx, w.rt)
	ok(t, err)
	w.work(t, plan, twoRepoPlan, "done")
	var c cards
	if did, aerr := store.AcceptPlan(ctx, w.rt, plan, c.ask, c.withdraw); aerr != nil || did != "accepted" {
		t.Fatalf("accept: %q %v", did, aerr)
	}
	rows, err := w.rt.Query(ctx, `SELECT t.repo, t.base_commit, b.body FROM agentrt.task t
	  JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id WHERE t.mission_id = $1 AND t.role = 'implement' ORDER BY t.repo`, mission)
	ok(t, err)
	var got []string
	for rows.Next() {
		var repo, base string
		var b []byte
		ok(t, rows.Scan(&repo, &base, &b))
		got = append(got, repo+"|"+base+"|"+string(b))
	}
	rows.Close()
	platform := store.Repos["bifrost-platform"]
	platformSpec, err := store.ImplementSpec("Name the test", "Name it in repos.go.", platform,
		append(slices.Clone(platform.Verify), []string{"go", "-C", "api", "test", "./internal/agentrt/store/"}))
	ok(t, err)
	infraSpec, err := store.ImplementSpec("Test the probe", "Add a test to scripts/agentrt.", store.Repos[infra], store.Repos[infra].Verify)
	ok(t, err)
	if want := []string{"bifrost-platform|" + commitA + "|" + string(platformSpec), infra + "|" + commitB + "|" + string(infraSpec)}; !slices.Equal(got, want) {
		t.Fatalf("the implementations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A mission of two repositories that kept no base for the second (one a
// route proposed, before goals kept bases): a plan with a task there is sent
// back, never started from the other repository's commit.
func TestPlanWithoutABaseIsSentBack(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-3", Title: "goal", Goal: "a goal",
			Deadline: time.Now().Add(24 * time.Hour), Repos: []string{"bifrost-platform", infra}, Success: []string{store.GoalSuccess},
			MaxAttempts: 10, MaxWallHours: 24}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-3", "active", "test")
	}))
	plan := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal", BaseCommit: commitA})
	w.work(t, plan, twoRepoPlan, "done")
	var c cards
	if did, err := store.AcceptPlan(ctx, w.rt, plan, c.ask, c.withdraw); err != nil || did != "rework" ||
		!strings.Contains(lastReason(t, w, plan), "tasks[0]: the goal recorded no commit of bifrost-trade-infra to start from") {
		t.Fatalf("a plan without a base: %q %v %q", did, err, lastReason(t, w, plan))
	}
	var n int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = 'M-3' AND role = 'implement'`).Scan(&n))
	if n != 0 {
		t.Fatalf("a refused plan made %d tasks", n)
	}
}

// oldGoalAnswer is an answer as the code before goals named several
// repositories stored it for a goal of the platform: no repos, no bases, and
// the waiting a proposed mission's answer carries (review 333 B1).
const oldGoalAnswer = `{"base_commit": "` + commitA + `", "mission_id": "M-9", "mission_state": "proposed", "repo": "bifrost-platform", ` +
	`"spec_artifact_id": "sha256:` + "0000000000000000000000000000000000000000000000000000000000000000" + `", "task_id": "t_old", ` +
	`"waiting": "no standing-envelope:dev is in force: the Owner starts or stops it"}`

// A key stored before goals named several repositories (its request {goal,
// repo}, its answer oldGoalAnswer) still answers, by repos of one, with all
// it stored and nothing else: no main read, nothing written (reviews 332 A1,
// 333 B1).
func TestGoalKeyStoredBeforeRepos(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	owner := store.Writer{Actor: "owner", Token: "admin"}
	sum, err := canon.Sum(map[string]any{"goal": "Document the goal route", "repo": "bifrost-platform"})
	ok(t, err)
	_, err = w.rt.Exec(ctx, `INSERT INTO agentrt.idempotency_key (idempotency_key_id, route, request_sha256, response_json, expires_at)
	  VALUES ('owner|goals|old', 'goals', $1, $2::jsonb, clock_timestamp() + interval '7 days')`, sum, oldGoalAnswer)
	ok(t, err)
	count := func() string {
		var n string
		ok(t, w.rt.QueryRow(ctx, `SELECT concat_ws(' ', (SELECT count(*) FROM agentrt.mission), (SELECT count(*) FROM agentrt.task),
		  (SELECT count(*) FROM agentrt.event), (SELECT count(*) FROM agentrt.artifact_blob), (SELECT count(*) FROM agentrt.idempotency_key))`).Scan(&n))
		return n
	}
	before := count()
	read := false
	main := func(context.Context, string) (string, error) { read = true; return commitB, nil }
	out, err := store.ProposeGoal(ctx, w.rt, owner, "old", store.Goal{Title: "Document the goal route", Text: "Document the goal route",
		Repos: []string{"bifrost-platform"}, Main: main})
	ok(t, err)
	var want map[string]any
	ok(t, json.Unmarshal([]byte(oldGoalAnswer), &want))
	if !reflect.DeepEqual(out, want) || read {
		t.Fatalf("the stored answer: %v, want %v (main read: %v)", out, want, read)
	}
	if after := count(); after != before {
		t.Fatalf("a stored key wrote: missions, tasks, events, artifacts, keys %s -> %s", before, after)
	}
}

// T26's first run (PROD M-9): a planner that read the goal's repositories
// side by side gave a platform task work only an infra checkout could do. A
// task's text that names a repository beside its checkout that the checkout
// does not have is refused, with what the task has; the siblings it has, its
// own repository and paths that name no repository pass.
func TestPlanTasksWorkInTheirCheckout(t *testing.T) {
	t.Parallel()
	text := func(repo, s string) string {
		return strings.Replace(twoRepoPlan, `"repo": "`+repo+`", "text": "`, `"repo": "`+repo+`", "text": "`+s+` `, 1)
	}
	both := []string{"bifrost-platform", infra}
	for name, c := range map[string]struct{ report, why string }{
		"platform runs infra": {text("bifrost-platform", "Then run ../bifrost-trade-infra/scripts/agentrt/check.sh."),
			"tasks[1]: its text names ../bifrost-trade-infra, which a task in bifrost-platform does not have " +
				"(it has bifrost-platform, with ../bifrost-ui at the head of its main beside it); what needs bifrost-trade-infra is a task in bifrost-trade-infra"},
		"infra reads the ui": {text(infra, "Compare with ../../bifrost-ui/src."), "tasks[0]: its text names ../bifrost-ui, which a task in bifrost-trade-infra does not have (it has bifrost-trade-infra alone)"},
		"another repository": {text("bifrost-platform", "See ../bifrost-research."), "names ../bifrost-research"},
		"a longer name":      {text("bifrost-platform", "Not ../bifrost-platform-plugin."), "names ../bifrost-platform-plugin"},
	} {
		if _, err := store.ParsePlan([]byte(c.report), both); err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: %v (want %q)", name, err, c.why)
		}
	}
	for name, report := range map[string]string{
		"its sibling":     text("bifrost-platform", "The UI is at ../bifrost-ui (file:../../bifrost-ui)."),
		"its own":         text("bifrost-platform", "Not ../bifrost-platform."),
		"a relative path": text("bifrost-platform", "Import it from ../lib/x and run go -C api test ../internal."),
		"a name, no path": text("bifrost-platform", "After t1 lands in bifrost-trade-infra."),
	} {
		if _, err := store.ParsePlan([]byte(report), both); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	if got := store.CheckoutOf(infra); got != "bifrost-trade-infra alone" {
		t.Errorf("CheckoutOf(infra) = %q", got)
	}
}
