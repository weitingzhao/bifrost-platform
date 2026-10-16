package agentrtapi_test

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentd"
)

// A goal over HTTP (W-31 2.2b; the Owner's D1 A): who may give one, what it
// becomes (a mission and one planning task on main, recorded as the caller),
// a spec agentd reads with the goal, the snapshot and the plan's format, a
// retry, a reused key, and refusals.
func TestGoalRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	mains = nil
	goal := "Add a test for goalTitle in the agentrtapi package\n\nCover a long first line and a goal of one line."
	body := map[string]any{"idempotency_key": "g1", "goal": "  " + goal + "\n", "repo": "bifrost-platform"}

	for _, who := range []string{"viewer", "agentd:h1", ""} {
		if code, _ := call(t, s, who, "POST", "goals", body, session, "s1"); code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("%q gave a goal: %d", who, code)
		}
	}
	code, out := call(t, s, "admin", "POST", "goals", body)
	if code != http.StatusOK || out["mission_id"] != "M-2" || out["base_commit"] != commitA || out["repo"] != "bifrost-platform" {
		t.Fatalf("a goal: %d %v", code, out)
	}
	// No standing envelope in this world: the mission waits, and says why.
	if out["mission_state"] != "proposed" || !strings.Contains(out["waiting"].(string), "standing-envelope") {
		t.Fatalf("the mission's state: %v", out)
	}
	if !slices.Equal(mains, []string{"bifrost-platform"}) {
		t.Fatalf("main read for %v", mains)
	}
	var missionBy, title, missionGoal string
	must(t, pool.QueryRow(ctx, `SELECT created_by_actor_id, title, goal FROM agentrt.mission WHERE mission_id = 'M-2'`).Scan(&missionBy, &title, &missionGoal))
	if missionBy != "owner" || title != "Add a test for goalTitle in the agentrtapi package" || missionGoal != goal {
		t.Fatalf("the mission: by %s, title %q, goal %q", missionBy, title, missionGoal)
	}
	var role, class, effect, repo, base, state, by, offPlan string
	var spec []byte
	must(t, pool.QueryRow(ctx, `SELECT t.role, t.task_class, t.effect_class, t.repo, t.base_commit, t.state, t.created_by_actor_id,
	  t.off_plan_reason, b.body FROM agentrt.task t JOIN agentrt.artifact_blob b ON b.artifact_id = t.spec_artifact_id
	  WHERE t.task_id = $1 AND t.mission_id = 'M-2'`, out["task_id"]).Scan(&role, &class, &effect, &repo, &base, &state, &by, &offPlan, &spec))
	if role != "plan" || class != "goal" || effect != "none" || repo != "bifrost-platform" || base != commitA ||
		state != "proposed" || by != "owner" || offPlan == "" {
		t.Fatalf("the task: %s %s %s %s %s %s %s %q", role, class, effect, repo, base, state, by, offPlan)
	}
	// The spec is what agentd runs: it parses there and has no checks (a plan
	// is checked by the controller); it carries the goal, the snapshot with the
	// repository's checks, and the plan's format.
	ts, err := agentd.ParseSpec(spec)
	must(t, err)
	if len(ts.Verify) != 0 {
		t.Fatalf("a plan's spec has checks %v", ts.Verify)
	}
	for _, want := range []string{goal, "Missions open besides this one", "go -C api build ./...", "```json", `"decisions"`} {
		if !strings.Contains(ts.Text, want) {
			t.Fatalf("spec text has no %q:\n%s", want, ts.Text)
		}
	}

	// A retry answers the same ids and writes nothing more.
	var missions, tasks int
	count := func() (int, int) {
		var m, k int
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.mission), (SELECT count(*) FROM agentrt.task)`).Scan(&m, &k))
		return m, k
	}
	missions, tasks = count()
	if code, again := call(t, s, "admin", "POST", "goals", body); code != http.StatusOK || again["mission_id"] != "M-2" || again["task_id"] != out["task_id"] {
		t.Fatalf("a retry: %d %v", code, again)
	}
	if m, k := count(); m != missions || k != tasks {
		t.Fatalf("a retry wrote: missions %d -> %d, tasks %d -> %d", missions, m, tasks, k)
	}
	// The same key with another goal is another request.
	body["goal"] = "something else"
	if code, out := call(t, s, "admin", "POST", "goals", body); code != http.StatusConflict || out["error"] != "key_conflict" {
		t.Fatalf("a reused key: %d %v", code, out)
	}
	// A thread gives goals too, recorded as itself.
	body["idempotency_key"] = "g2"
	if code, out := call(t, s, "operator", "POST", "goals", body, session, "s1"); code != http.StatusOK || out["mission_id"] != "M-3" {
		t.Fatalf("an operator's goal: %d %v", code, out)
	}
	must(t, pool.QueryRow(ctx, `SELECT created_by_actor_id FROM agentrt.mission WHERE mission_id = 'M-3'`).Scan(&by))
	if by != "thread:s1" {
		t.Fatalf("an operator's goal recorded as %s", by)
	}

	missions, tasks = count()
	for name, c := range map[string]struct {
		body map[string]any
		has  string
	}{
		"empty":        {map[string]any{"idempotency_key": "x1", "goal": " \n\t ", "repo": "bifrost-platform"}, "says what to do"},
		"too long":     {map[string]any{"idempotency_key": "x2", "goal": strings.Repeat("字", 2001), "repo": "bifrost-platform"}, "at most 2000"},
		"control char": {map[string]any{"idempotency_key": "x3", "goal": "do \x1b[31mthis", "repo": "bifrost-platform"}, "control character"},
		"other repo":   {map[string]any{"idempotency_key": "x4", "goal": "do this", "repo": "bifrost-trade-api"}, "may name bifrost-platform"},
		"no repo":      {map[string]any{"idempotency_key": "x5", "goal": "do this"}, "may name"},
		"no key":       {map[string]any{"goal": "do this", "repo": "bifrost-platform"}, "idempotency key"},
	} {
		if code, out := call(t, s, "admin", "POST", "goals", c.body); code != http.StatusUnprocessableEntity || out["error"] != "invalid" ||
			!strings.Contains(out["message"].(string), c.has) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if code, _ := call(t, s, "admin", "POST", "goals", map[string]any{"goal": "x", "repo": "bifrost-platform", "idempotency_key": "x6", "surprise": 1}); code != http.StatusBadRequest {
		t.Errorf("an unknown field: %d", code)
	}
	// A main that cannot be read refuses before anything is written.
	mainFails = true
	t.Cleanup(func() { mainFails = false })
	if code, out := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "x7", "goal": "do this", "repo": "bifrost-platform"}); code != http.StatusServiceUnavailable || out["error"] != "no_main" {
		t.Errorf("main unreadable: %d %v", code, out)
	}
	if m, k := count(); m != missions || k != tasks {
		t.Fatalf("a refused goal wrote: missions %d -> %d, tasks %d -> %d", missions, m, tasks, k)
	}
}

// Without a reader for main the route answers 503 and writes nothing.
func TestGoalWithoutMainReader(t *testing.T) {
	pool, _ := world(t)
	noMain = true
	t.Cleanup(func() { noMain = false })
	s := server(t, pool)
	if code, out := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "g", "goal": "do this", "repo": "bifrost-platform"}); code != http.StatusServiceUnavailable || out["error"] != "no_main_reader" {
		t.Fatalf("no reader: %d %v", code, out)
	}
	var n int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.mission`).Scan(&n))
	if n != 1 {
		t.Fatalf("missions: %d", n)
	}
}

// A long first line is cut to the title's length; the goal keeps all of it.
func TestGoalTitleIsCut(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	long := strings.Repeat("a", 100)
	code, out := call(t, s, "admin", "POST", "goals", map[string]any{"idempotency_key": "g", "goal": long, "repo": "bifrost-platform"})
	if code != http.StatusOK {
		t.Fatalf("a goal: %d %v", code, out)
	}
	var title, goal string
	must(t, pool.QueryRow(ctx, `SELECT title, goal FROM agentrt.mission WHERE mission_id = $1`, out["mission_id"]).Scan(&title, &goal))
	if title != strings.Repeat("a", 79)+"…" || goal != long {
		t.Fatalf("title %q goal %q", title, goal)
	}
}

// A retry after main moved: the goal was written in one transaction on the
// first try, so the retry gets that answer, its base among it, and writes
// nothing (review 213 B2's case cannot leave a mission without its task).
func TestGoalRetryAfterMainMoved(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	const commitB = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"
	t.Cleanup(func() { mainAt = commitA })
	body := map[string]any{"idempotency_key": "g1", "goal": "Name the default checks in the goal's report", "repo": "bifrost-platform"}
	code, first := call(t, s, "admin", "POST", "goals", body)
	if code != http.StatusOK || first["base_commit"] != commitA {
		t.Fatalf("a goal: %d %v", code, first)
	}
	mainAt = commitB
	code, again := call(t, s, "admin", "POST", "goals", body)
	if code != http.StatusOK || again["mission_id"] != first["mission_id"] || again["task_id"] != first["task_id"] ||
		again["base_commit"] != commitA || again["spec_artifact_id"] != first["spec_artifact_id"] {
		t.Fatalf("a retry on a moved main: %d %v (first %v)", code, again, first)
	}
	var tasks int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.task WHERE mission_id = $1`, first["mission_id"]).Scan(&tasks))
	if tasks != 1 {
		t.Fatalf("tasks of the goal: %d", tasks)
	}
}

// The same goal sent twice at once (review 213 B1): one mission, one task,
// both answers name them.
func TestGoalConcurrentDuplicates(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	body := map[string]any{"idempotency_key": "g1", "goal": "Add a doc comment to MainHead", "repo": "bifrost-platform"}
	var wg sync.WaitGroup
	answers := make([]map[string]any, 4)
	codes := make([]int, 4)
	for i := range answers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i], answers[i] = call(t, s, "admin", "POST", "goals", body)
		}(i)
	}
	wg.Wait()
	for i := range answers {
		if codes[i] != http.StatusOK || answers[i]["mission_id"] != answers[0]["mission_id"] || answers[i]["task_id"] != answers[0]["task_id"] {
			t.Fatalf("answer %d: %d %v (first %v)", i, codes[i], answers[i], answers[0])
		}
	}
	var m, k int
	must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.mission WHERE mission_id <> 'M-1'), (SELECT count(*) FROM agentrt.task WHERE mission_id <> 'M-1')`).Scan(&m, &k))
	if m != 1 || k != 1 {
		t.Fatalf("missions %d, tasks %d", m, k)
	}
}
