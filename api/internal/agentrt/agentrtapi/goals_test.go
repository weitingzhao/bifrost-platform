package agentrtapi_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

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
	t.Parallel()
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
	// Main unreadable (review 216 B1): a key with an answer still gets it, a
	// reused key with another goal is still a conflict, a new key waits.
	mainFails = true
	t.Cleanup(func() { mainFails = false })
	if code, out := call(t, s, "admin", "POST", "goals", body); code != http.StatusOK || out["task_id"] != first["task_id"] {
		t.Fatalf("a retry while main cannot be read: %d %v", code, out)
	}
	other := map[string]any{"idempotency_key": "g1", "goal": "another goal", "repo": "bifrost-platform"}
	if code, out := call(t, s, "admin", "POST", "goals", other); code != http.StatusConflict || out["error"] != "key_conflict" {
		t.Fatalf("a reused key while main cannot be read: %d %v", code, out)
	}
	other["idempotency_key"] = "g2"
	if code, out := call(t, s, "admin", "POST", "goals", other); code != http.StatusServiceUnavailable || out["error"] != "no_main" {
		t.Fatalf("a new goal while main cannot be read: %d %v", code, out)
	}
}

// The same goal sent twice at once (review 213 B1): one mission, one task,
// both answers name them.
func TestGoalConcurrentDuplicates(t *testing.T) {
	t.Parallel()
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
