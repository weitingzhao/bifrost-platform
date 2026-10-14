package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
)

const session = "X-Bifrost-Session"

// Proposing a mission over HTTP: who may, who it is recorded as, a retry and
// a reused key, and refusals with their reasons.
func TestProposeMissionRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	body := map[string]any{"idempotency_key": "k1", "title": "m", "goal": "g", "success": []string{"it works"},
		"budget": map[string]any{"max_attempts": 40, "max_wall_hours": 48}}

	for _, who := range []string{"viewer", "agentd:h1", ""} {
		if code, _ := call(t, s, who, "POST", "missions", body, session, "s1"); code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("%q proposed a mission: %d", who, code)
		}
	}
	if code, out := call(t, s, "operator", "POST", "missions", body); code != http.StatusUnprocessableEntity || out["error"] != "invalid" {
		t.Fatalf("a thread without a session: %d %v", code, out)
	}
	status, first := call(t, s, "operator", "POST", "missions", body, session, "s1")
	if status != http.StatusOK || first["mission_id"] != "M-2" || first["state"] != "proposed" {
		t.Fatalf("propose: %d %v", status, first)
	}
	if code, again := call(t, s, "operator", "POST", "missions", body, session, "s1"); code != http.StatusOK || again["mission_id"] != "M-2" {
		t.Fatalf("a retry: %d %v", code, again)
	}
	body["goal"] = "another"
	if code, out := call(t, s, "operator", "POST", "missions", body, session, "s1"); code != http.StatusConflict || out["error"] != "key_conflict" {
		t.Fatalf("a reused key: %d %v", code, out)
	}
	// The key is the thread's: another thread with the same key is another proposal.
	if code, out := call(t, s, "operator", "POST", "missions", body, session, "s2"); code != http.StatusOK || out["mission_id"] != "M-3" {
		t.Fatalf("another thread: %d %v", code, out)
	}
	for who, want := range map[string]string{"admin": "owner", "seat:chief_of_staff": "seat:chief_of_staff", "reporter": "thread:s3"} {
		body["idempotency_key"] = "k-" + who
		got, out := call(t, s, who, "POST", "missions", body, session, "s3")
		if got != http.StatusOK {
			t.Fatalf("%s: %d %v", who, got, out)
		}
		var by string
		must(t, pool.QueryRow(ctx, `SELECT created_by_actor_id FROM agentrt.mission WHERE mission_id = $1`, out["mission_id"]).Scan(&by))
		if by != want {
			t.Errorf("%s wrote as %s; want %s", who, by, want)
		}
	}
	body["idempotency_key"] = "k-ghost"
	if code, out := call(t, s, "seat:ghost", "POST", "missions", body); code != http.StatusUnprocessableEntity {
		t.Fatalf("an unregistered seat: %d %v", code, out)
	}
	body["max_action"] = "external"
	if code, out := call(t, s, "operator", "POST", "missions", body, session, "s1"); code != http.StatusUnprocessableEntity ||
		!strings.Contains(out["message"].(string), "max_action") {
		t.Fatalf("external: %d %v", code, out)
	}
	if code, _ := call(t, s, "operator", "POST", "missions", map[string]any{"title": "m", "surprise": 1}, session, "s1"); code != http.StatusBadRequest {
		t.Fatalf("an unknown field: %d", code)
	}
	status, m := call(t, s, "viewer", "GET", "missions/M-2", nil)
	if status != http.StatusOK || m["state"] != "proposed" || m["created_by_actor_id"] != "thread:s1" {
		t.Fatalf("read: %d %v", status, m)
	}
	if code, _ := call(t, s, "viewer", "GET", "missions/M-99", nil); code != http.StatusNotFound {
		t.Fatalf("a missing mission: %d", code)
	}
}

// Proposing tasks over HTTP: the spec is uploaded first, the batch is all or
// nothing, a guard of the database answers 422 with the rule it broke (T32
// through the interface), and the list reads them back.
func TestProposeTasksRoute(t *testing.T) {
	pool, existing := world(t)
	s := server(t, pool)
	spec := []byte("# implement x\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec"); code != http.StatusOK {
		t.Fatalf("an operator uploads a spec: %d %v", code, out)
	}
	task := func(ref, step string, deps ...string) map[string]any {
		return map[string]any{"ref": ref, "plan_node_id": step, "role": "implement", "task_class": "go", "title": ref,
			"spec_artifact_id": artifactID(spec), "effect_class": "branch", "repo": "bifrost-platform", "base_commit": commitA,
			"depends_on": deps}
	}
	body := map[string]any{"idempotency_key": "k1", "tasks": []any{task("a", "R2:1-1"), task("b", "R2:1-1", "a", existing)}}
	if code, _ := call(t, s, "viewer", "POST", "missions/M-1/tasks", body); code != http.StatusUnauthorized {
		t.Fatalf("a viewer proposed: %d", code)
	}
	status, out := call(t, s, "operator", "POST", "missions/M-1/tasks", body, session, "s1")
	if status != http.StatusOK || len(out["tasks"].([]any)) != 2 {
		t.Fatalf("propose: %d %v", status, out)
	}
	var before int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.task`).Scan(&before))
	for name, c := range map[string]struct {
		task       map[string]any
		code       int
		error, has string
	}{
		"on an item":  {task("x", "R2:1"), http.StatusUnprocessableEntity, "constraint", "step"},
		"bad commit":  {func() map[string]any { m := task("x", "R2:1-1"); m["base_commit"] = "main"; return m }(), http.StatusUnprocessableEntity, "constraint", "base_commit"},
		"a review":    {func() map[string]any { m := task("x", "R2:1-1"); m["role"] = "review"; return m }(), http.StatusUnprocessableEntity, "invalid", "role"},
		"no such dep": {task("x", "R2:1-1", "nope"), http.StatusUnprocessableEntity, "invalid", "nope"},
	} {
		got, out := call(t, s, "operator", "POST", "missions/M-1/tasks",
			map[string]any{"idempotency_key": "r-" + name, "tasks": []any{task("fine", "R2:1-1"), c.task}}, session, "s1")
		if got != c.code || out["error"] != c.error || !strings.Contains(out["message"].(string), c.has) {
			t.Errorf("%s: %d %v", name, got, out)
		}
	}
	var after int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.task`).Scan(&after))
	if after != before {
		t.Fatalf("refused batches wrote %d tasks", after-before)
	}
	if code, out := call(t, s, "operator", "POST", "missions/M-9/tasks", body, session, "s1"); code != http.StatusNotFound {
		t.Fatalf("a missing mission: %d %v", code, out)
	}

	status, list := call(t, s, "viewer", "GET", "tasks?mission=M-1&state=proposed", nil)
	if status != http.StatusOK || len(list["tasks"].([]any)) != 2 || list["truncated"] != false {
		t.Fatalf("list: %d %v", status, list)
	}
	if code, _ := call(t, s, "viewer", "GET", "tasks?state=done", nil); code != http.StatusBadRequest {
		t.Fatalf("a state there is not: %d", code)
	}
	status, m := call(t, s, "viewer", "GET", "missions/M-1", nil)
	counts, _ := m["task_counts"].(map[string]any)
	if status != http.StatusOK || counts["proposed"] != float64(2) || counts["ready"] != float64(1) {
		t.Fatalf("mission: %d %v", status, m)
	}
}
