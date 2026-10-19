package agentrtapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
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
	// No standing envelope is in force: the mission waits on the Owner's card.
	cards, askFails = nil, false
	status, first := call(t, s, "operator", "POST", "missions", body, session, "s1")
	if status != http.StatusOK || first["mission_id"] != "M-2" || first["state"] != "awaiting_owner" || first["question_id"] != "q_1" ||
		len(cards) != 1 || cards[0].Title != "Mission: m" || cards[0].Options[0] != store.MissionStart {
		t.Fatalf("propose: %d %v, cards %+v", status, first, cards)
	}
	if code, again := call(t, s, "operator", "POST", "missions", body, session, "s1"); code != http.StatusOK || again["mission_id"] != "M-2" ||
		len(cards) != 1 {
		t.Fatalf("a retry: %d %v, %d cards", code, again, len(cards))
	}
	// The Owner cannot be asked: refused whole, to be tried again.
	askFails = true
	body["idempotency_key"] = "k-unreachable"
	if code, out := call(t, s, "operator", "POST", "missions", body, session, "s1"); code != http.StatusServiceUnavailable || out["error"] != "owner_unreachable" {
		t.Fatalf("the Owner unreachable: %d %v", code, out)
	}
	askFails = false
	// Words longer than a card holds are refused, whatever the envelope
	// (Owner 2026-10-15 「决策卡 A」), with what to do instead.
	for name, edit := range map[string]func(b map[string]any){
		"title":   func(b map[string]any) { b["title"] = strings.Repeat("t", store.MaxMissionTitleBytes+1) },
		"goal":    func(b map[string]any) { b["goal"] = strings.Repeat("目", store.MaxMissionGoalBytes/3+1) },
		"success": func(b map[string]any) { b["success"] = []string{strings.Repeat("s", store.MaxMissionSuccessBytes+1)} },
	} {
		long := map[string]any{"idempotency_key": "k-long-" + name, "title": "m", "goal": "g", "success": []string{"it works"}}
		edit(long)
		code, out := call(t, s, "operator", "POST", "missions", long, session, "s1")
		if msg, _ := out["message"].(string); code != http.StatusUnprocessableEntity || !strings.Contains(msg, "put the details in a document") {
			t.Errorf("a long %s: %d %v", name, code, out)
		}
	}
	body["idempotency_key"] = "k1"
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
	if status != http.StatusOK || m["state"] != "awaiting_owner" || m["created_by_actor_id"] != "thread:s1" {
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
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
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

// Review 175 B4: an upload is recorded as its writer, like a proposal; one
// whose writer may not write leaves nothing, not even a thread.
func TestUploadAttribution(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	for i, c := range []struct{ who, session, actor string }{
		{"operator", "s1", "thread:s1"},
		{"reporter", "s2", "thread:s2"},
		{"admin", "", "owner"},
		{"seat:chief_of_staff", "", "seat:chief_of_staff"},
		{"agentd:h1", "", "host:h1"},
	} {
		body := []byte(fmt.Sprintf("# upload %d\n", i))
		header := []string{"X-Artifact-Kind", "report"}
		if c.session != "" {
			header = append(header, session, c.session)
		}
		if code, out := call(t, s, c.who, "PUT", "artifacts/"+artifactID(body), body, header...); code != http.StatusOK || out["created"] != true {
			t.Fatalf("%s: %d %v", c.who, code, out)
		}
		var by, actor, token string
		must(t, pool.QueryRow(ctx, `SELECT a.created_by_actor_id, e.actor_id, e.body_json->>'uploaded_by' FROM agentrt.artifact a
		  JOIN agentrt.event e ON e.event_type = 'artifact_put' AND e.body_json->>'artifact_id' = a.artifact_id
		  WHERE a.artifact_id = $1`, artifactID(body)).Scan(&by, &actor, &token))
		if by != c.actor || actor != c.actor || token != c.who {
			t.Errorf("%s: artifact by %s, event by %s, uploaded_by %s", c.who, by, actor, token)
		}
	}
	var actors, events int
	count := func() {
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.actor), (SELECT count(*) FROM agentrt.event)`).Scan(&actors, &events))
	}
	count()
	before := [2]int{actors, events}
	body := []byte("# refused\n")
	for _, c := range []struct{ who, session string }{{"reporter", ""}, {"seat:ghost", ""}, {"operator", "not a session!"}} {
		if code, out := call(t, s, c.who, "PUT", "artifacts/"+artifactID(body), body, "X-Artifact-Kind", "report", session, c.session); code != http.StatusUnprocessableEntity {
			t.Errorf("%s/%q: %d %v", c.who, c.session, code, out)
		}
	}
	count()
	if [2]int{actors, events} != before {
		t.Fatalf("refused uploads left actors or events: %v -> %v", before, [2]int{actors, events})
	}
}

// Review 175 B5: hard_limit_s is bounded before it becomes a duration.
func TestHardLimitBounds(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	spec := []byte("# spec\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
		t.Fatalf("spec: %d %v", code, out)
	}
	for i, c := range []struct {
		limit any
		code  int
		want  float64
	}{
		{-1, http.StatusUnprocessableEntity, 0},
		{86401, http.StatusUnprocessableEntity, 0},
		{2147483648, http.StatusUnprocessableEntity, 0},
		{18446744074, http.StatusUnprocessableEntity, 0},
		// 2^55 + 60 seconds wraps, as nanoseconds, to exactly 60 seconds:
		// only the bound before the conversion refuses it.
		{36028797018964028, http.StatusUnprocessableEntity, 0},
		{0, http.StatusOK, 3600},
		{86400, http.StatusOK, 86400},
	} {
		body := map[string]any{"idempotency_key": fmt.Sprint("k", i), "tasks": []any{map[string]any{"ref": "a", "plan_node_id": "R2:1-1",
			"role": "implement", "task_class": "go", "title": "a", "spec_artifact_id": artifactID(spec), "effect_class": "branch",
			"hard_limit_s": c.limit}}}
		code, out := call(t, s, "operator", "POST", "missions/M-1/tasks", body, session, "s1")
		if code != c.code {
			t.Errorf("hard_limit_s %v: %d %v", c.limit, code, out)
			continue
		}
		if code == http.StatusOK {
			_, task := call(t, s, "viewer", "GET", "tasks/"+out["tasks"].([]any)[0].(map[string]any)["task_id"].(string), nil)
			if task["hard_limit_s"] != c.want {
				t.Errorf("hard_limit_s %v stored as %v", c.limit, task["hard_limit_s"])
			}
		}
	}
}

// A batch whose step stays locked answers 503 with Retry-After, not 500.
func TestBusyIs503(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	spec := []byte("# busy\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
		t.Fatalf("spec: %d %v", code, out)
	}
	tx, err := pool.Begin(ctx)
	must(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = 'R2:1-1' FOR UPDATE`)
	must(t, err)
	code, out := call(t, s, "operator", "POST", "missions/M-1/tasks", map[string]any{"idempotency_key": "k", "tasks": []any{map[string]any{
		"ref": "a", "plan_node_id": "R2:1-1", "role": "implement", "task_class": "go", "title": "a",
		"spec_artifact_id": artifactID(spec), "effect_class": "branch"}}}, session, "s1")
	if code != http.StatusServiceUnavailable || out["error"] != "busy" {
		t.Fatalf("a batch on a held step: %d %v", code, out)
	}
}

// Review 178 A1: the same tasks proposed again with a new key, after the
// first were cancelled, are new tasks; a retry with the old key is answered
// with the old ones.
func TestProposeAgainAfterCancel(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	spec := []byte("# again\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
		t.Fatalf("spec: %d %v", code, out)
	}
	body := map[string]any{"idempotency_key": "p-1", "tasks": []any{map[string]any{"ref": "a", "plan_node_id": "R2:1-1", "role": "implement",
		"task_class": "go", "title": "a", "spec_artifact_id": artifactID(spec), "effect_class": "branch"}}}
	_, first := call(t, s, "operator", "POST", "missions/M-1/tasks", body, session, "s1")
	old := first["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MoveTask(ctx, tx, "controller", old, "cancelled", "test") }))
	if _, again := call(t, s, "operator", "POST", "missions/M-1/tasks", body, session, "s1"); again["tasks"].([]any)[0].(map[string]any)["task_id"] != old {
		t.Fatalf("a retry of the first proposal: %v", again)
	}
	body["idempotency_key"] = "p-2"
	_, second := call(t, s, "operator", "POST", "missions/M-1/tasks", body, session, "s1")
	fresh := second["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	var state string
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, fresh).Scan(&state))
	if fresh == old || state != "proposed" {
		t.Fatalf("the proposal made again: %s (%s), the first was %s", fresh, state, old)
	}
}

// POST /plan/proposals: within the standing authorization the change is
// applied at once; outside it the Owner gets a card and nothing changes;
// a change the database refuses is 422 with its reason; a question store
// that cannot take the card is 503.
func TestPlanProposalRoute(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	cards, askFails = nil, false
	body := func(key string, changes ...map[string]any) map[string]any {
		return map[string]any{"idempotency_key": key, "root": "R2", "reason": "test", "changes": changes}
	}
	step := map[string]any{"op": "add_node", "parent": "R2:1", "kind": "step", "step_kind": "agent", "title": "next", "mission_id": "M-1"}
	if code, _ := call(t, s, "viewer", "POST", "plan/proposals", body("k0", step), session, "s1"); code != http.StatusUnauthorized {
		t.Fatalf("a viewer proposed a plan change: %d", code)
	}
	// R2:1 has no active step yet: outside the standing authorization.
	code, out := call(t, s, "operator", "POST", "plan/proposals", body("k1", step), session, "s1")
	if code != http.StatusOK || out["decision"] != "" || out["question_id"] != "q_1" || len(cards) != 1 {
		t.Fatalf("outside: %d %v, %d cards", code, out, len(cards))
	}
	if !strings.Contains(cards[0].Body, "add_node") || !strings.HasPrefix(cards[0].Title, "Plan change to R2") {
		t.Fatalf("the card: %+v", cards[0])
	}
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", "R2", "R2:1-1", "active", "test")
		return err
	}))
	code, out = call(t, s, "operator", "POST", "plan/proposals", body("k2", step), session, "s1")
	if nodes, _ := out["nodes"].([]any); code != http.StatusOK || out["decision"] != "applied" || len(nodes) != 1 || nodes[0] != "R2:1-2" {
		t.Fatalf("standing: %d %v", code, out)
	}
	code, out = call(t, s, "operator", "POST", "plan/proposals",
		body("k3", map[string]any{"op": "add_node", "parent": "R2", "kind": "step", "step_kind": "agent", "title": "x"}), session, "s1")
	if code != http.StatusUnprocessableEntity || out["error"] != "constraint" || !strings.Contains(out["message"].(string), "cannot sit under") {
		t.Fatalf("a step under a round: %d %v", code, out)
	}
	if code, out = call(t, s, "operator", "POST", "plan/proposals", body("k4", map[string]any{"op": "add_node", "parent": "R2", "kind": "item",
		"title": "y", "surprise": 1}), session, "s1"); code != http.StatusBadRequest {
		t.Fatalf("an unknown field in a change: %d %v", code, out)
	}
	askFails = true
	defer func() { askFails = false }()
	code, out = call(t, s, "operator", "POST", "plan/proposals", body("k5", map[string]any{"op": "add_node", "parent": "R2", "kind": "item", "title": "z"}), session, "s1")
	if code != http.StatusServiceUnavailable || out["error"] != "owner_unreachable" {
		t.Fatalf("a card the store refuses: %d %v", code, out)
	}
}
