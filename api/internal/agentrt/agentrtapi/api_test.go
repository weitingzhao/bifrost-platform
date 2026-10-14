package agentrtapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var ctx = context.Background()

const commitA = "2333e00753f91c74c8b2f4d693a803f8e89e7892"

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// world is a migrated database with an active mission, a step, host h1, the
// implementer job with cursor as its candidate, and one ready task.
func world(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	db := agentrttest.Migrated(t)
	_, err := store.Seed(ctx, db.Connect(t, db.Owner))
	must(t, err)
	pool := db.Pool(t, db.Runtime)
	tx := func(fn func(pgx.Tx) error) { t.Helper(); must(t, store.InTx(ctx, pool, fn)) }
	tx(func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-1", Title: "m", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-1", "active", "test")
	})
	for _, n := range []store.PlanNode{
		{ID: "R2", Kind: "round", Label: "2", Title: "round", Root: "R2"},
		{ID: "R2:1", Parent: "R2", Kind: "item", Label: "1", Title: "item", Root: "R2"},
		{ID: "R2:1-1", Parent: "R2:1", Kind: "step", Label: "1-1", Title: "step", MissionID: "M-1", StepKind: "agent", Root: "R2"},
	} {
		tx(func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err })
	}
	must(t, store.RegisterHost(ctx, pool, "owner", "h1", "dev"))
	tx(func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs"}}); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO agentrt.rule (rule_id, rule_version, body_json, body_text, direction, effective_at, expires_at)
			 VALUES ('job:test', 1, '{}', 'test', 'tighten', now(), now() + interval '1 year')`,
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('implementer', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status)
			 VALUES ('implementer/exec:cursor/cursor-mix', 'implementer', 'exec:cursor', 'cursor-mix', 'active')`,
		} {
			if _, err := tx.Exec(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	task := store.NewTaskID()
	tx(func(tx pgx.Tx) error {
		spec, err := store.PutArtifact(ctx, tx, "controller", "task_spec", "text/markdown", []byte("# task\n"))
		if err != nil {
			return err
		}
		if _, err = store.ProposeTask(ctx, tx, "controller", store.Task{ID: task, MissionID: "M-1", PlanNode: "R2:1-1", Role: "implement",
			Class: "go", Title: "t", SpecArtifact: spec, Effect: "branch", Repo: "bifrost-platform", BaseCommit: commitA}); err != nil {
			return err
		}
		return store.MarkReady(ctx, tx, "controller", task)
	})
	return pool, task
}

// Tokens of the fixture auth file, by caller name.
var tokens = map[string]string{"agentd:h1": "fixture-agentd-h1", "agentd:h2": "fixture-agentd-h2", "viewer": "fixture-viewer-1"}

// server serves the API behind the platform's own authentication, loaded
// from a fixture file: two agentd tokens bound to h1 and h2, and a viewer.
func server(t *testing.T, db store.DB) *httptest.Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.yaml")
	must(t, os.WriteFile(path, []byte(`tokens:
  - name: agentd:h1
    role: agentd
    token: fixture-agentd-h1
  - name: agentd:h2
    role: agentd
    token: fixture-agentd-h2
  - name: viewer
    role: viewer
    token: fixture-viewer-1
`), 0o600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	api := &agentrtapi.API{DB: db, ClaimWait: 300 * time.Millisecond, ClaimPoll: 50 * time.Millisecond,
		Head: func(context.Context, string, string) (string, error) { return commitA, nil }}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { agentrtapi.Mount(r, auth, api) })
	s := httptest.NewServer(r)
	t.Cleanup(s.Close)
	return s
}

func call(t *testing.T, s *httptest.Server, caller, method, path string, body any, header ...string) (int, map[string]any) {
	t.Helper()
	var r *bytes.Reader
	switch b := body.(type) {
	case []byte:
		r = bytes.NewReader(b)
	case nil:
		r = bytes.NewReader(nil)
	default:
		doc, err := json.Marshal(b)
		must(t, err)
		r = bytes.NewReader(doc)
	}
	req, err := http.NewRequest(method, s.URL+"/api/v1/agentrt/"+path, r)
	must(t, err)
	if caller != "" {
		req.Header.Set("Authorization", "Bearer "+tokens[caller])
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func artifactID(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// agentd's whole path over HTTP: claim, start, heartbeat, checkpoint,
// upload, computed, persist, submit; retries answered from the key.
func TestAgentdPath(t *testing.T) {
	pool, task := world(t)
	s := server(t, pool)
	cursor := map[string]any{"executor": "exec:cursor", "provider": "cursor-mix"}
	claimBody := map[string]any{"host_id": "h1", "free_slots": 1, "tools": []any{cursor}}

	if code, _ := call(t, s, "viewer", "POST", "claim", claimBody); code != http.StatusUnauthorized {
		t.Fatalf("a viewer claimed: %d", code)
	}
	if code, out := call(t, s, "agentd:h2", "POST", "claim", claimBody); code != http.StatusForbidden || out["error"] != "wrong_host" {
		t.Fatalf("a token of h2 claimed for h1: %d %v", code, out)
	}
	status, claim := call(t, s, "agentd:h1", "POST", "claim", claimBody)
	if status != http.StatusOK || claim["task_id"] != task || claim["base_commit"] != commitA || claim["repo"] != "bifrost-platform" {
		t.Fatalf("claim: %d %v", status, claim)
	}
	attempt := claim["attempt_id"].(string)
	fence := func(extra map[string]any) map[string]any {
		b := map[string]any{"host_id": "h1", "lease_epoch": claim["lease_epoch"]}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	for _, step := range []struct {
		action string
		body   map[string]any
	}{
		{"start", fence(map[string]any{"model_provider": "cursor-mix"})},
		{"heartbeat", fence(map[string]any{"phase": "running", "line": "compiling"})},
	} {
		if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body); code != http.StatusOK {
			t.Fatalf("%s: %d %v", step.action, code, out)
		}
	}
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/heartbeat",
		map[string]any{"host_id": "h1", "lease_epoch": 999}); code != http.StatusConflict || out["error"] != "fenced" {
		t.Fatalf("an old epoch: %d %v", code, out)
	}

	report := []byte("# done\nall tests pass\n")
	leaky := []byte("# done\nsee /Users/someone/work\n")
	if code, out := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", "report"); code != http.StatusUnprocessableEntity {
		t.Fatalf("a report with a local path: %d %v", code, out)
	}
	if code, _ := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(leaky), report, "X-Artifact-Kind", "report"); code != http.StatusBadRequest {
		t.Fatalf("a body under another hash: %d", code)
	}
	if code, _ := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "task_spec"); code != http.StatusForbidden {
		t.Fatalf("agentd uploaded a task spec: %d", code)
	}
	if code, out := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "report", "Content-Type", "text/markdown"); code != http.StatusOK {
		t.Fatalf("upload: %d %v", code, out)
	}
	exit := 0
	for _, step := range []struct {
		action string
		body   map[string]any
	}{
		{"checkpoint", fence(map[string]any{"checkpoint_seq": 1, "artifacts": []string{artifactID(report)}})},
		{"computed", fence(map[string]any{"exit_code": exit, "end_how": "exit", "conclusion": "done"})},
		{"persist", fence(map[string]any{"report": artifactID(report), "pushed_ref": claim["pushed_ref"], "pushed_commit": commitA})},
		{"submit", fence(nil)},
		{"submit", fence(nil)},
	} {
		if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body); code != http.StatusOK {
			t.Fatalf("%s: %d %v", step.action, code, out)
		}
	}
	status, got := call(t, s, "viewer", "GET", "tasks/"+task, nil)
	if status != http.StatusOK || got["state"] != "submitted" {
		t.Fatalf("task: %d %v", status, got)
	}
	if code, _ := call(t, s, "", "GET", "tasks/"+task, nil); code != http.StatusUnauthorized {
		t.Fatalf("no token read a task: %d", code)
	}
	if code, out := call(t, s, "agentd:h1", "POST", "claim", claimBody); code != http.StatusNoContent {
		t.Fatalf("an empty queue answered %d %v", code, out)
	}
}

// With no database the routes answer 503 and nothing else happens.
func TestNotConfigured(t *testing.T) {
	s := server(t, nil)
	if code, out := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1"}); code != http.StatusServiceUnavailable {
		t.Fatalf("%d %v", code, out)
	}
}
