package agentrtapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// TD-370's ratchet: a thread's session is the token's that first wrote as
// it. A second token naming the session is refused (403 not_yours) on every
// write and on GET /messages, a replay of the first token's key included,
// and whoami says why; the first token goes on writing and reading.
func TestSessionHeldToItsToken(t *testing.T) {
	pool, existing := world(t)
	s := server(t, pool)
	as := func(caller, method, path string, body any) (int, map[string]any) {
		t.Helper()
		if method == "PUT" {
			return call(t, s, caller, method, path, body, session, "s1", "X-Artifact-Kind", "task_spec")
		}
		return call(t, s, caller, method, path, body, session, "s1")
	}
	mission := map[string]any{"idempotency_key": "k1", "title": "m", "goal": "g", "success": []string{"it works"}}
	if code, out := as("operator", "POST", "missions", mission); code != http.StatusOK {
		t.Fatalf("the first write registers the session: %d %v", code, out)
	}
	spec := []byte("# a spec\n")
	if code, out := as("operator", "PUT", "artifacts/"+artifactID(spec), spec); code != http.StatusOK {
		t.Fatalf("an upload: %d %v", code, out)
	}
	if code, out := call(t, s, "admin", "POST", "messages", map[string]any{"to": "thread:s1", "kind": "note", "body": "for s1"}); code != http.StatusOK {
		t.Fatalf("a message to the thread: %d %v", code, out)
	}
	_, inbox := as("operator", "GET", "messages", nil)
	msgs, _ := inbox["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("the thread's mail: %v", inbox)
	}
	msg := msgs[0].(map[string]any)["message_id"]
	other := []byte("# another spec\n")
	for name, c := range map[string]struct {
		caller, method, path string
		body                 any
	}{
		"a mission":           {"reporter", "POST", "missions", map[string]any{"idempotency_key": "k2", "title": "m", "goal": "g", "success": []string{"x"}}},
		"a replay of its key": {"reporter", "POST", "missions", mission},
		"tasks": {"reporter", "POST", "missions/M-1/tasks", map[string]any{"idempotency_key": "k3", "tasks": []any{map[string]any{
			"ref": "a", "plan_node_id": "R2:1-1", "role": "implement", "task_class": "go", "title": "a", "spec_artifact_id": artifactID(spec),
			"effect_class": "branch", "repo": "bifrost-platform", "base_commit": commitA, "depends_on": []string{existing}}}}},
		"a goal": {"reporter", "POST", "goals", map[string]any{"idempotency_key": "k4", "goal": "do a thing", "repo": "bifrost-platform"}},
		"a plan change": {"reporter", "POST", "plan/proposals", map[string]any{"idempotency_key": "k5", "root": "R2", "reason": "rename",
			"changes": []any{map[string]any{"op": "retitle", "node": "R2:1", "title": "x"}}}},
		"a control":            {"reporter", "POST", "controls", map[string]any{"idempotency_key": "k6", "scope": "global", "kind": "pause", "reason": "test"}},
		"a new upload":         {"reporter", "PUT", "artifacts/" + artifactID(other), other},
		"an upload it has":     {"reporter", "PUT", "artifacts/" + artifactID(spec), spec},
		"a message":            {"reporter", "POST", "messages", map[string]any{"to": "owner", "kind": "note", "body": "hi"}},
		"an acknowledgement":   {"reporter", "POST", "messages/ack", map[string]any{"message_ids": []any{msg}}},
		"its mail":             {"reporter", "GET", "messages", nil},
		"its mail by agentd":   {"agentd:h1", "GET", "messages", nil},
		"its mail by a viewer": {"viewer", "GET", "messages", nil},
		"a rule proposal": {"reporter", "POST", "rules/routing:dev/proposals", map[string]any{"idempotency_key": "k7", "reason": "test",
			"text": "rule_id: routing:dev\nrule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2030-01-01T00:00:00Z\n" +
				"tenant: dev\norder: [capability, quota]\nquota_stop_percent: 90\n"}},
	} {
		code, out := as(c.caller, c.method, c.path, c.body)
		if code != http.StatusForbidden || out["error"] != "not_yours" {
			t.Errorf("%s by another token: %d %v; want 403 not_yours", name, code, out)
		}
	}
	if code, out := as("reporter", "GET", "whoami", nil); code != http.StatusOK || !strings.Contains(out["actor_error"].(string), "another token") {
		t.Errorf("whoami for another token: %d %v", code, out)
	}
	// The first token goes on: its mail is still unread, and it may write.
	if code, out := as("operator", "GET", "messages", nil); code != http.StatusOK || len(out["messages"].([]any)) != 1 {
		t.Fatalf("the thread's own mail after the refusals: %d %v", code, out)
	}
	if code, out := as("operator", "POST", "messages/ack", map[string]any{"message_ids": []any{msg}}); code != http.StatusOK {
		t.Fatalf("its own acknowledgement: %d %v", code, out)
	}
	if code, out := as("operator", "POST", "missions", mission); code != http.StatusOK || out["mission_id"] != "M-2" {
		t.Fatalf("its own replay: %d %v", code, out)
	}
	// Another session of the second token is its own thread.
	if code, out := call(t, s, "reporter", "GET", "messages", nil, session, "s2"); code != http.StatusOK {
		t.Fatalf("another session: %d %v", code, out)
	}
}
