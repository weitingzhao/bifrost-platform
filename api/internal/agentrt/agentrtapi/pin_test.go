package agentrtapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The coordinator's pin over HTTP (DESIGN-coordinator-landing-v3 §2.2, §6
// T1, T2).

const pinEnvelopeText = `rule_id: standing-envelope:dev
rule_version: %d
direction: %s
effective_at: 2026-10-01T00:00:00Z
expires_at: 2100-01-01T00:00:00Z
tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:claude]
hosts: [coordinator-mbp]
`

const pinHosts = `external_hosts:
  - host: coordinator-mbp
    pin: external:coordinator
    proposer: seat:coordinator
    executor: exec:claude
    provider: anthropic
    repos: [bifrost-platform]
    roles: [implement]
`

// pinWorld is world with the coordinator's seat and the external host
// registered and the envelope naming them in force, as if the Owner had
// signed it.
func pinWorld(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := world(t)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "actor_registered", Actor: "controller",
			Body: map[string]any{"actors": []string{"seat:coordinator"}, "note": "test: the coordinator's seat"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.actor (actor_id, actor_kind, display_name) VALUES ('seat:coordinator', 'seat', 'Coordinator')`)
		return err
	}))
	must(t, store.RegisterHost(ctx, pool, "owner", "coordinator-mbp", store.HostExternal))
	putEnvelope(t, pool, 1, rules.Initial, pinHosts)
	return pool
}

// putEnvelope stores a version of the dev envelope as if the Owner had
// signed it.
func putEnvelope(t *testing.T, pool *pgxpool.Pool, version int, direction, hosts string) {
	t.Helper()
	text := []byte(fmt.Sprintf(pinEnvelopeText, version, direction) + hosts)
	r, err := rules.Parse(text)
	must(t, err)
	body, err := r.BodyJSON()
	must(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) VALUES ('standing-envelope:dev', $1, $2, $3, $4, 'test', 'test', $5, $6)`,
		version, string(body), string(text), direction, r.Effective, r.Expires)
	must(t, err)
}

func TestPinOverHTTP(t *testing.T) {
	t.Parallel()
	pool := pinWorld(t)
	s := server(t, pool)
	spec := []byte("# implement x on the coordinator's Mac\n")
	if code, out := call(t, s, "operator", "PUT", "artifacts/"+artifactID(spec), spec, "X-Artifact-Kind", "task_spec", session, "s1"); code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, out)
	}
	body := func(key string) map[string]any {
		return map[string]any{"idempotency_key": key, "tasks": []any{map[string]any{"ref": "a", "plan_node_id": "R2:1-1", "role": "implement",
			"task_class": "go", "title": "a", "spec_artifact_id": artifactID(spec), "effect_class": "branch", "repo": "bifrost-platform",
			"base_commit": commitA, "executor_pin": rules.PinCoordinator}}}
	}
	counts := func() (tasks, events int) {
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.task), (SELECT count(*) FROM agentrt.event)`).Scan(&tasks, &events))
		return
	}

	// T1: the shared operator token with the coordinator's session header
	// is a thread, not the proposer seat.
	t0, e0 := counts()
	if code, out := call(t, s, "operator", "POST", "missions/M-1/tasks", body("k-op"), session, "local_coordinator"); code != http.StatusUnprocessableEntity ||
		out["error"] != "pin_not_permitted" {
		t.Fatalf("an operator with the coordinator's session: %d %v", code, out)
	}
	if t1, e1 := counts(); t1 != t0 || e1 != e0 {
		t.Fatalf("the refusal left %d tasks and %d events", t1-t0, e1-e0)
	}

	// The proposer seat's own token: pinned, the session recorded.
	code, out := call(t, s, "seat:coordinator", "POST", "missions/M-1/tasks", body("k1"), session, "local_coordinator")
	if code != http.StatusOK {
		t.Fatalf("the proposer seat: %d %v", code, out)
	}
	id := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	var pin, by string
	var ev map[string]any
	must(t, pool.QueryRow(ctx, `SELECT t.executor_pin, t.created_by_actor_id, e.body_json FROM agentrt.task t JOIN agentrt.event e
	  ON e.task_id = t.task_id AND e.event_type = 'task_proposed' WHERE t.task_id = $1`, id).Scan(&pin, &by, &ev))
	if pin != rules.PinCoordinator || by != "seat:coordinator" || ev["executor_pin"] != rules.PinCoordinator || ev["via_session"] != "local_coordinator" {
		t.Fatalf("pinned task: pin %q by %s, event %v", pin, by, ev)
	}
	if code, got := call(t, s, "viewer", "GET", "tasks/"+id, nil); code != http.StatusOK || got["executor_pin"] != rules.PinCoordinator {
		t.Fatalf("read back: %d %v", code, got)
	}

	// T2: a version of the envelope without external hosts, signed: the
	// same proposal by the same seat is refused, nothing written.
	putEnvelope(t, pool, 2, rules.Tighten, "")
	t0, e0 = counts()
	if code, out := call(t, s, "seat:coordinator", "POST", "missions/M-1/tasks", body("k2"), session, "local_coordinator"); code != http.StatusUnprocessableEntity ||
		out["error"] != "pin_not_permitted" || !strings.Contains(out["message"].(string), "names no external host") {
		t.Fatalf("with external_hosts dropped: %d %v", code, out)
	}
	if t1, e1 := counts(); t1 != t0 || e1 != e0 {
		t.Fatalf("the refusal left %d tasks and %d events", t1-t0, e1-e0)
	}
}
