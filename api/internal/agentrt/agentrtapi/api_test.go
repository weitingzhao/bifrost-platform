package agentrtapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	_, ferr := pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, ferr)
	tx(func(tx pgx.Tx) error {
		if err := store.LockCapabilities(ctx, tx); err != nil {
			return err
		}
		if _, err := store.Append(ctx, tx, store.Event{Stream: "system", Type: "legacy_imported", Actor: "controller",
			Body: map[string]any{"source": "test: jobs"}}); err != nil {
			return err
		}
		for _, q := range []string{
			agentrttest.TestJobRule,
			`INSERT INTO agentrt.job (job_id, tenant, rule_id, rule_version) VALUES ('implementer', 'dev', 'job:test', 1)`,
			`INSERT INTO agentrt.job_candidate (job_candidate_id, job_id, executor_actor_id, model_provider, status, rule_id, rule_version)
			 VALUES ('implementer/exec:cursor/cursor-mix', 'implementer', 'exec:cursor', 'cursor-mix', 'active', 'job:test', 1)`,
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
var tokens = map[string]string{"agentd:h1": "fixture-agentd-h1", "agentd:h2": "fixture-agentd-h2", "agentd:h3": "fixture-agentd-h3", "viewer": "fixture-viewer-1",
	"reporter": "fixture-reporter-1", "operator": "fixture-operator-1", "admin": "fixture-admin-1",
	"seat:chief_of_staff": "fixture-seat-1", "seat:ghost": "fixture-seat-2"}

// heads records the branch heads the API asked for, as repo|ref.
var heads []string

// cards records the decision cards the API asked the Owner; askFails makes
// the next asks fail, as a full question store would; anchor is the key a
// signed rule must verify against.
var (
	cards    []store.Card
	askFails bool
	anchor   string
)

// server serves the API behind the platform's own authentication, loaded
// from a fixture file: two agentd tokens bound to h1 and h2, a viewer, a
// reporter, an operator, an admin and two seat tokens (one registered).
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
  - name: agentd:h3
    role: agentd
    token: fixture-agentd-h3
  - name: viewer
    role: viewer
    token: fixture-viewer-1
  - name: reporter
    role: reporter
    token: fixture-reporter-1
  - name: operator
    role: operator
    token: fixture-operator-1
  - name: admin
    role: admin
    token: fixture-admin-1
  - name: seat:chief_of_staff
    role: reporter
    token: fixture-seat-1
  - name: seat:ghost
    role: reporter
    token: fixture-seat-2
`), 0o600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	api := &agentrtapi.API{DB: db, ClaimWait: 300 * time.Millisecond, ClaimPoll: 50 * time.Millisecond,
		Head: func(_ context.Context, repo, ref string) (string, error) {
			heads = append(heads, repo+"|"+ref)
			return commitA, nil
		},
		Ask: func(_ context.Context, c store.Card) (string, error) {
			if askFails {
				return "", fmt.Errorf("store full")
			}
			cards = append(cards, c)
			return fmt.Sprintf("q_%d", len(cards)), nil
		}, Anchor: anchor}
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
	for _, half := range []map[string]any{{"executor": "exec:cursor"}, {"provider": "cursor-mix"}} {
		if code, _ := call(t, s, "agentd:h1", "POST", "claim", map[string]any{"host_id": "h1", "free_slots": 1,
			"tools": []any{half}}); code != http.StatusBadRequest {
			t.Fatalf("a tool %v was answered %d", half, code)
		}
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
		{"heartbeat", fence(nil)},
	} {
		if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body); code != http.StatusOK {
			t.Fatalf("%s: %d %v", step.action, code, out)
		}
	}
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/heartbeat",
		map[string]any{"host_id": "h1", "lease_epoch": 999}); code != http.StatusConflict || out["error"] != "fenced" {
		t.Fatalf("an old epoch: %d %v", code, out)
	}
	for _, field := range []string{"phase", "line"} {
		if code, _ := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/heartbeat", fence(map[string]any{field: "x"})); code != http.StatusBadRequest {
			t.Fatalf("a %s nothing keeps was answered %d", field, code)
		}
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
	// A3: the first upload of a body is recorded once; uploading it again is not.
	if code, out := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(report), report, "X-Artifact-Kind", "report"); code != http.StatusOK || out["created"] != false {
		t.Fatalf("a second upload: %d %v", code, out)
	}
	var puts int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'artifact_put' AND body_json->>'artifact_id' = $1
	  AND body_json->>'uploaded_by' = 'agentd:h1' AND actor_id = 'host:h1'`, artifactID(report)).Scan(&puts))
	if puts != 1 {
		t.Fatalf("%d artifact_put events for one body", puts)
	}
	// A1: raw output H8 lets through cannot take the report's place.
	log := []byte("compiling in /Users/someone/work\n")
	if code, _ := call(t, s, "agentd:h1", "PUT", "artifacts/"+artifactID(log), log, "X-Artifact-Kind", "log_tail"); code != http.StatusOK {
		t.Fatalf("a log tail: %d", code)
	}
	exit := 0
	for _, step := range []struct {
		action string
		body   map[string]any
	}{
		{"checkpoint", fence(map[string]any{"checkpoint_seq": 1, "artifacts": []string{artifactID(report)}})},
		{"computed", fence(map[string]any{"exit_code": exit, "end_how": "exit", "conclusion": "done"})},
	} {
		if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/"+step.action, step.body); code != http.StatusOK {
			t.Fatalf("%s: %d %v", step.action, code, out)
		}
	}
	heads = nil
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/persist",
		fence(map[string]any{"report": artifactID(log), "pushed_ref": claim["pushed_ref"], "pushed_commit": commitA})); code != http.StatusConflict || out["error"] != "state" {
		t.Fatalf("a log tail as the report: %d %v", code, out)
	}
	if fmt.Sprint(heads) != fmt.Sprintf("[bifrost-platform|%s]", claim["pushed_ref"]) {
		t.Fatalf("branch heads read: %v", heads)
	}
	// A report stored by another path (not an upload) is scanned when it is bound.
	var planted string
	must(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		planted, err = store.PutArtifact(ctx, tx, "controller", "report", "text/markdown", []byte("# done\nsee C:\\work\n"))
		return err
	}))
	if code, out := call(t, s, "agentd:h1", "POST", "attempts/"+attempt+"/persist",
		fence(map[string]any{"report": planted, "pushed_ref": claim["pushed_ref"], "pushed_commit": commitA})); code != http.StatusConflict || out["error"] != "state" {
		t.Fatalf("a report with a local path: %d %v", code, out)
	}
	for _, step := range []struct {
		action string
		body   map[string]any
	}{
		{"persist", fence(map[string]any{"report": artifactID(report), "artifacts": []string{artifactID(log)},
			"pushed_ref": claim["pushed_ref"], "pushed_commit": commitA})},
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

// Uploads: every kind but the four H8 lets through is scanned, whoever
// uploads it; a kind the schema does not have is refused (review 156 A2, B3).
func TestUploadKinds(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	leaky := []byte("input at /home/ci/private-input\n")
	for _, kind := range []string{"mission_spec", "message", "exam", "report"} {
		if code, _ := call(t, s, "reporter", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", kind, "X-Bifrost-Session", "s-rep"); code != http.StatusUnprocessableEntity {
			t.Errorf("a reporter's %s with a local path: %d", kind, code)
		}
	}
	for _, kind := range []string{"", "notes"} {
		if code, _ := call(t, s, "reporter", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", kind, "X-Bifrost-Session", "s-rep"); code != http.StatusBadRequest {
			t.Errorf("kind %q: %d", kind, code)
		}
	}
	if code, out := call(t, s, "reporter", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", "brief", "X-Bifrost-Session", "s-rep"); code != http.StatusOK || out["created"] != true {
		t.Errorf("a brief, which H8 lets through: %d %v", code, out)
	}
	var by string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'uploaded_by' FROM agentrt.event WHERE event_type = 'artifact_put'
	  AND body_json->>'artifact_id' = $1`, artifactID(leaky)).Scan(&by))
	if by != "reporter" {
		t.Errorf("uploaded_by = %q", by)
	}
}

// Review 157 B1: the same body uploaded twice at once is stored and
// recorded once.
func TestUploadRace(t *testing.T) {
	pool, _ := world(t)
	s := server(t, pool)
	body := []byte("# race\n")
	created := make(chan any, 2)
	for _, who := range []string{"agentd:h1", "reporter"} {
		go func() {
			req, err := http.NewRequest("PUT", s.URL+"/api/v1/agentrt/artifacts/"+artifactID(body), bytes.NewReader(body))
			if err != nil {
				created <- err
				return
			}
			req.Header.Set("Authorization", "Bearer "+tokens[who])
			req.Header.Set("X-Artifact-Kind", "report")
			req.Header.Set("X-Bifrost-Session", "s-race")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				created <- err
				return
			}
			defer func() { _ = resp.Body.Close() }()
			var out map[string]any
			_ = json.NewDecoder(resp.Body).Decode(&out)
			created <- out["created"]
		}()
	}
	got := fmt.Sprint(<-created, " ", <-created)
	var puts int
	must(t, pool.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'artifact_put' AND body_json->>'artifact_id' = $1`,
		artifactID(body)).Scan(&puts))
	if (got != "true false" && got != "false true") || puts != 1 {
		t.Fatalf("created %s, %d artifact_put", got, puts)
	}
}

// notYet is a database the platform has not connected yet.
type notYet struct{}

func (notYet) Ready() bool { return false }

func (notYet) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, fmt.Errorf("not connected")
}

// Until the platform has connected the database, the routes answer 503
// not_connected (it keeps connecting in the background).
func TestNotConnected(t *testing.T) {
	s := server(t, notYet{})
	if code, out := call(t, s, "viewer", "GET", "progress", nil); code != http.StatusServiceUnavailable || out["error"] != "not_connected" {
		t.Fatalf("%d %v", code, out)
	}
}
