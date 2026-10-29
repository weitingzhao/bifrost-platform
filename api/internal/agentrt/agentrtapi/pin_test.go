package agentrtapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
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
	registerCoordinatorSeat(t, pool)
	must(t, store.RegisterHost(ctx, pool, "owner", "coordinator-mbp", store.HostExternal))
	putEnvelope(t, pool, 1, rules.Initial, pinHosts)
	return pool
}

// putEnvelope stores a version of the dev envelope as if the Owner had
// signed it.
func putEnvelope(t *testing.T, pool *pgxpool.Pool, version int, direction, hosts string) {
	t.Helper()
	putRule(t, pool, fmt.Sprintf(pinEnvelopeText, version, direction)+hosts)
}

// putRule stores a rule's text as if the Owner had signed it.
func putRule(t *testing.T, pool *pgxpool.Pool, text string) {
	t.Helper()
	r, err := rules.Parse([]byte(text))
	must(t, err)
	body, err := r.BodyJSON()
	must(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, signed_by, signature,
	  effective_at, expires_at) VALUES ($1, $2, $3, $4, $5, 'test', 'test', $6, $7)`,
		r.ID, r.Version, string(body), text, r.Direction, r.Effective, r.Expires)
	must(t, err)
}

// registerCoordinatorSeat registers seat:coordinator as the platform does
// at its start for a configured seat token (store.EnsureSeats).
func registerCoordinatorSeat(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := store.EnsureSeats(ctx, pool, []string{"seat:coordinator"})
	must(t, err)
}

// pinnedFixture is a ready task of mission M-90 (tenant business) pinned to
// coordinator-mbp: the business envelope names the host, its budget rule is
// in force and the implementer job has a claude candidate. It returns the
// task.
func pinnedFixture(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	registerCoordinatorSeat(t, pool)
	head := "rule_version: 1\ndirection: initial\neffective_at: 2026-10-01T00:00:00Z\nexpires_at: 2100-01-01T00:00:00Z\ntenant: business\n"
	putRule(t, pool, "rule_id: budget:business\n"+head+"mission: {max_attempts: 100, max_wall_hours: 100}\nattempt_hard_limit_s: 86400\n")
	putRule(t, pool, "rule_id: standing-envelope:business\n"+head+`max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:claude]
hosts: [coordinator-mbp]
`+pinHosts)
	claudeImplementer(t, pool)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-90", Title: "pinned", Goal: "g", Tenant: "business",
			Deadline: time.Now().Add(time.Hour), MaxAttempts: 10, MaxWallHours: 10}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-90", "active", "test")
	}))
	out, err := store.ProposePinnedTasks(ctx, pool, store.Writer{Actor: "seat:coordinator", Token: "seat:coordinator"}, "pinned-fixture", "M-90", "",
		[]store.PinnedTask{{TaskProposal: store.TaskProposal{Ref: "a", OffPlanReason: "handed in", Role: "implement", Class: "go", Title: "a",
			Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}, ExecutorPin: rules.PinCoordinator, Providers: []string{"anthropic"}}})
	must(t, err)
	task := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MarkReady(ctx, tx, "controller", task) }))
	return task
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
			"base_commit": commitA, "executor_pin": rules.PinCoordinator, "providers": []string{"anthropic"}}}}
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

// claudeImplementer makes claude of anthropic a candidate of the implementer
// job, as the pin's entry names it.
func claudeImplementer(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: a claude implementer"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version)
		  VALUES ('implementer/exec:claude/anthropic', 'implementer', 'exec:claude', 'anthropic', 'active', 'job:test', 1) ON CONFLICT DO NOTHING`)
		return err
	}))
}

// T5, T7, T9, T14 and T15 over HTTP: the exact claim is the external host's
// alone and takes no tools; the external host's ordinary claim is refused
// with nothing written; its result is checked on the forge's commit.
func TestExactClaimOverHTTP(t *testing.T) {
	t.Parallel()
	pool := pinWorld(t)
	claudeImplementer(t, pool)
	facts := store.CommitFacts{Parents: []string{"1111111111111111111111111111111111111111"}, Paths: []string{"api/internal/research/x.go"}, Lines: 3}
	s := server(t, pool, func(a *agentrtapi.API) {
		a.Commits = func(context.Context, string, string) (store.CommitFacts, error) { return facts, nil }
	})
	out, err := store.ProposePinnedTasks(ctx, pool, store.Writer{Actor: "seat:coordinator", Token: "seat:coordinator"}, "k", "M-1", "",
		[]store.PinnedTask{{TaskProposal: store.TaskProposal{Ref: "a", PlanNode: "R2:1-1", Role: "implement", Class: "go", Title: "a",
			Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}, ExecutorPin: rules.PinCoordinator, Providers: []string{"anthropic"}}})
	must(t, err)
	task := out["tasks"].([]any)[0].(map[string]any)["task_id"].(string)
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { return store.MarkReady(ctx, tx, "controller", task) }))
	var plain string
	must(t, pool.QueryRow(ctx, `SELECT task_id FROM agentrt.task WHERE state = 'ready' AND executor_pin IS NULL`).Scan(&plain))
	counts := func() (attempts, events int) {
		must(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agentrt.attempt), (SELECT count(*) FROM agentrt.event)`).Scan(&attempts, &events))
		return
	}
	req := func(host, task, key string) map[string]any {
		return map[string]any{"host_id": host, "task_id": task, "idempotency_key": key, "free_slots": 1}
	}

	a0, e0 := counts()
	for name, c := range map[string]struct {
		caller string
		body   map[string]any
		code   int
		err    string
	}{
		"an ordinary agentd (T5)":  {"agentd:h1", req("h1", task, "k1"), http.StatusForbidden, "forbidden"},
		"a task with no pin (T7)":  {"agentd:coordinator-mbp", req("coordinator-mbp", plain, "k1"), http.StatusForbidden, "not_pinned"},
		"another host in the body": {"agentd:coordinator-mbp", req("h1", task, "k1"), http.StatusForbidden, "wrong_host"},
		"tools":                    {"agentd:coordinator-mbp", func() map[string]any { b := req("coordinator-mbp", task, "k1"); b["tools"] = []any{}; return b }(), http.StatusBadRequest, "bad_request"},
		"an operator":              {"operator", req("coordinator-mbp", task, "k1"), http.StatusUnauthorized, ""},
	} {
		code, out := call(t, s, c.caller, "POST", "claim/pinned", c.body)
		if code != c.code || (c.err != "" && out["error"] != c.err) {
			t.Errorf("%s: %d %v", name, code, out)
		}
	}
	// T9: the external host's ordinary claim, refused before it writes.
	if code, out := call(t, s, "agentd:coordinator-mbp", "POST", "claim", map[string]any{"host_id": "coordinator-mbp", "free_slots": 1,
		"tools": []any{map[string]any{"executor": "exec:claude", "provider": "anthropic"}}}); code != http.StatusForbidden || out["error"] != "external_host" {
		t.Errorf("the external host's ordinary claim: %d %v", code, out)
	}
	if a, e := counts(); a != a0 || e != e0 {
		t.Fatalf("the refusals left %d attempts and %d events", a-a0, e-e0)
	}

	code, claim := call(t, s, "agentd:coordinator-mbp", "POST", "claim/pinned", req("coordinator-mbp", task, "k1"))
	if code != http.StatusOK || claim["task_id"] != task || claim["pushed_ref"] != store.AttemptRef(task, 1) {
		t.Fatalf("the exact claim: %d %v", code, claim)
	}
	if code, again := call(t, s, "agentd:coordinator-mbp", "POST", "claim/pinned", req("coordinator-mbp", task, "k1")); code != http.StatusOK ||
		again["attempt_id"] != claim["attempt_id"] {
		t.Fatalf("its retry: %d %v", code, again)
	}
	attempt := claim["attempt_id"].(string)
	fence := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "coordinator-mbp", "lease_epoch": claim["lease_epoch"]}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	act := func(action string, body map[string]any) (int, map[string]any) {
		return call(t, s, "agentd:coordinator-mbp", "POST", "attempts/"+attempt+"/"+action, body)
	}
	report := []byte("# handed in\n")
	if code, out := call(t, s, "agentd:coordinator-mbp", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "report"); code != http.StatusOK {
		t.Fatalf("report: %d %v", code, out)
	}
	persist := fence(map[string]any{"report": artifactID(report), "pushed_ref": store.AttemptRef(task, 1), "pushed_commit": pushedAt})
	if code, out := act("start", fence(map[string]any{"model_provider": "anthropic"})); code != http.StatusOK {
		t.Fatalf("start: %d %v", code, out)
	}
	if code, out := act("persist", persist); code != http.StatusConflict { // T15: before computed
		t.Fatalf("persist before computed: %d %v", code, out)
	}
	if code, out := act("computed", fence(map[string]any{"end_how": "exit", "exit_code": 0, "conclusion": "done"})); code != http.StatusOK {
		t.Fatalf("computed: %d %v", code, out)
	}
	// T14: the forge's commit is on another parent: refused, then the commit on the base persists.
	if code, out := act("persist", persist); code != http.StatusConflict || out["error"] != "state" {
		t.Fatalf("a commit on another parent: %d %v", code, out)
	}
	facts = store.CommitFacts{Parents: []string{commitA}, Message: "x\n\nAgentrt-Task: " + task + "\nChange-Id: I" + strings.Repeat("cd", 20) + "\n",
		Paths: []string{"api/internal/agentrt/store/x.go"}, Lines: 3}
	if code, out := act("persist", persist); code != http.StatusUnprocessableEntity || out["error"] != "core_mismatch" { // T16
		t.Fatalf("core work in a task that is not core: %d %v", code, out)
	}
	facts.Paths = []string{"api/internal/research/x.go"}
	if code, out := act("persist", persist); code != http.StatusOK {
		t.Fatalf("persist: %d %v", code, out)
	}
	if code, out := act("submit", fence(nil)); code != http.StatusOK {
		t.Fatalf("submit: %d %v", code, out)
	}
}
