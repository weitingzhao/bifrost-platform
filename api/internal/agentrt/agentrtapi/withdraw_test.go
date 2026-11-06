package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// withdrawWorld is world with tasks a, b (depending on a) and c proposed by
// the operator's thread s1, a and c ready; it gives their ids by ref and the
// fixture's own task, the controller's.
func withdrawWorld(t *testing.T) (*pgxpool.Pool, map[string]string, string) {
	t.Helper()
	pool, own := world(t)
	s := server(t, pool)
	spec := implSpec(t, "# withdraw me\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
		t.Fatalf("spec: %d %v", code, out)
	}
	task := func(ref string, deps ...string) map[string]any {
		return map[string]any{"ref": ref, "plan_node_id": "R2:1-1", "role": "implement", "task_class": "go", "title": ref,
			"spec_artifact_id": artifactID(spec), "effect_class": "branch", "repo": "bifrost-platform", "base_commit": commitA,
			"depends_on": deps}
	}
	code, out := call(t, s, "operator", "POST", "missions/M-1/tasks",
		map[string]any{"idempotency_key": "p1", "tasks": []any{task("a"), task("b", "a"), task("c")}}, session, "s1")
	if code != http.StatusOK {
		t.Fatalf("propose: %d %v", code, out)
	}
	ids := map[string]string{}
	for _, e := range out["tasks"].([]any) {
		m := e.(map[string]any)
		ids[m["ref"].(string)] = m["task_id"].(string)
	}
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		for _, ref := range []string{"a", "c"} {
			if err := store.MarkReady(ctx, tx, "controller", ids[ref]); err != nil {
				return err
			}
		}
		return nil
	}))
	return pool, ids, own
}

func taskState(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var s string
	must(t, pool.QueryRow(ctx, `SELECT state FROM agentrt.task WHERE task_id = $1`, id).Scan(&s))
	return s
}

// 200 with what was withdrawn, the checks cancelled and by whom; a retry
// gets the same answer.
func TestWithdrawRouteWithdraws(t *testing.T) {
	pool, ids, _ := withdrawWorld(t)
	s := server(t, pool)
	body := map[string]any{"idempotency_key": "w1", "task_ids": []string{ids["c"]}, "reason": "the spec was wrong"}
	code, out := call(t, s, "operator", "POST", "tasks/withdraw", body, session, "s1")
	got, _ := out["withdrawn"].([]any)
	checks, isList := out["checks_cancelled"].([]any)
	if code != http.StatusOK || len(got) != 1 || got[0] != ids["c"] || !isList || len(checks) != 0 || out["by"] != "thread:s1" {
		t.Fatalf("withdraw: %d %v", code, out)
	}
	if st := taskState(t, pool, ids["c"]); st != "cancelled" {
		t.Fatalf("c is %s", st)
	}
	if again, retry := call(t, s, "operator", "POST", "tasks/withdraw", body, session, "s1"); again != http.StatusOK || retry["by"] != "thread:s1" {
		t.Fatalf("retry: %d %v", again, retry)
	}
}

// 400 for a request that breaks a rule of its own: no reason, no ids, an
// unknown field.
func TestWithdrawRouteInvalid(t *testing.T) {
	pool, ids, _ := withdrawWorld(t)
	s := server(t, pool)
	for name, body := range map[string]map[string]any{
		"no reason": {"idempotency_key": "w1", "task_ids": []string{ids["c"]}},
		"no ids":    {"idempotency_key": "w2", "reason": "why"},
		"no key":    {"task_ids": []string{ids["c"]}, "reason": "why"},
		"no task":   {"idempotency_key": "w3", "task_ids": []string{"t_nope"}, "reason": "why"},
		"a field":   {"idempotency_key": "w4", "task_ids": []string{ids["c"]}, "reason": "why", "force": true},
	} {
		if code, out := call(t, s, "operator", "POST", "tasks/withdraw", body, session, "s1"); code != http.StatusBadRequest {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	if st := taskState(t, pool, ids["c"]); st != "ready" {
		t.Fatalf("c is %s", st)
	}
}

// The refusals: another thread's task, or the controller's, is 403
// not_yours; a task with a dependent outside the list, or one whose work
// started, is 409 state.
func TestWithdrawRouteRefusals(t *testing.T) {
	pool, ids, own := withdrawWorld(t)
	s := server(t, pool)
	body := func(key string, tasks ...string) map[string]any {
		return map[string]any{"idempotency_key": key, "task_ids": tasks, "reason": "why"}
	}
	for name, c := range map[string]struct {
		caller, session string
		body            map[string]any
		code            int
		error, has      string
	}{
		"another thread":    {"operator", "s2", body("w1", ids["c"]), http.StatusForbidden, "not_yours", ids["c"]},
		"the Owner":         {"admin", "", body("w2", ids["c"]), http.StatusForbidden, "not_yours", ids["c"]},
		"the controller's":  {"operator", "s1", body("w3", own), http.StatusForbidden, "not_yours", own},
		"a dependent":       {"operator", "s1", body("w4", ids["a"]), http.StatusConflict, "state", ids["b"]},
		"a seat's not ours": {"seat:chief_of_staff", "", body("w5", ids["a"], ids["b"]), http.StatusForbidden, "not_yours", ""},
	} {
		var header []string
		if c.session != "" {
			header = []string{session, c.session}
		}
		code, out := call(t, s, c.caller, "POST", "tasks/withdraw", c.body, header...)
		msg, _ := out["message"].(string)
		if code != c.code || out["error"] != c.error || !strings.Contains(msg, c.has) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	// Work that started: a claimed by h1, the only ready task left.
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.MoveTask(ctx, tx, "controller", own, "cancelled", "test"); err != nil {
			return err
		}
		return store.MoveTask(ctx, tx, "controller", ids["c"], "paused", "test")
	}))
	claim := map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}}}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", claim); code != http.StatusOK || out["task_id"] != ids["a"] {
		t.Fatalf("claim: %d %v", code, out)
	}
	code, out := call(t, s, "operator", "POST", "tasks/withdraw", body("w6", ids["a"], ids["b"]), session, "s1")
	if code != http.StatusConflict || out["error"] != "state" || !strings.Contains(out["message"].(string), ids["a"]) {
		t.Fatalf("started work: %d %v", code, out)
	}
	for ref, want := range map[string]string{"a": "leased", "b": "proposed", "c": "paused"} {
		if st := taskState(t, pool, ids[ref]); st != want {
			t.Errorf("%s is %s; want %s", ref, st, want)
		}
	}
}

// An anonymous call is refused, as are a viewer and a reporter, who may not
// propose; nothing moves.
func TestWithdrawRouteAnonymous(t *testing.T) {
	pool, ids, _ := withdrawWorld(t)
	s := server(t, pool)
	body := map[string]any{"idempotency_key": "w1", "task_ids": []string{ids["c"]}, "reason": "why"}
	if code, out := call(t, s, "", "POST", "tasks/withdraw", body, session, "s1"); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d %v", code, out)
	}
	for _, who := range []string{"viewer", "reporter"} {
		if code, out := call(t, s, who, "POST", "tasks/withdraw", body, session, "s1"); code != http.StatusUnauthorized && code != http.StatusForbidden {
			t.Fatalf("%s: %d %v", who, code, out)
		}
	}
	if st := taskState(t, pool, ids["c"]); st != "ready" {
		t.Fatalf("c is %s", st)
	}
}
