//bifrost:owner plan

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
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/storekit"
)

var ctx = context.Background()

const commitA = "2333e00753f91c74c8b2f4d693a803f8e89e7892"

// pushedAt is the commit the fake Gitea says every branch is at: not the
// tasks' base commitA, since an implementation that pushes its base changed
// nothing and is refused (TD-360).
const pushedAt = "0b9e1f3c7a1d2e4f5a6b7c8d9e0f1a2b3c4d5e6f"

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
		if _, err := storekit.CreateMission(ctx, tx, "controller", storekit.Mission{ID: "M-1", Title: "m", Goal: "g", Deadline: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		return storekit.MoveMission(ctx, tx, "controller", "M-1", "active", "test")
	})
	for _, n := range []planstore.PlanNode{
		{ID: "R2", Kind: "round", Label: "2", Title: "round", Root: "R2"},
		{ID: "R2:1", Parent: "R2", Kind: "item", Label: "1", Title: "item", Root: "R2"},
		{ID: "R2:1-1", Parent: "R2:1", Kind: "step", Label: "1-1", Title: "step", MissionID: "M-1", StepKind: "agent", Root: "R2"},
	} {
		tx(func(tx pgx.Tx) error { _, err := planstore.CreatePlanNode(ctx, tx, "controller", n); return err })
	}
	must(t, seedHost(ctx, pool, "owner", "h1", "dev"))
	_, ferr := pool.Exec(ctx, agentrttest.DispatchRules)
	must(t, ferr)
	tx(func(tx pgx.Tx) error {
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
	// A task id no row answers to: the tasks' table is gone (0020).
	return pool, store.NewTaskID()
}

// Tokens of the fixture auth file, by caller name.
var tokens = map[string]string{"agentd:h1": "fixture-agentd-h1", "agentd:h2": "fixture-agentd-h2", "agentd:h3": "fixture-agentd-h3", "viewer": "fixture-viewer-1",
	"reporter": "fixture-reporter-1", "proposer": "fixture-proposer-1", "operator": "fixture-operator-1", "admin": "fixture-admin-1",
	"seat:chief_of_staff": "fixture-seat-1", "seat:ghost": "fixture-seat-2", "seat:coordinator": "fixture-seat-coordinator", "agentd:coordinator-mbp": "fixture-agentd-coordinator",
	// An operator not on the issue readers list (W-31 BI-1 slice 2) and an
	// intake client's token, read from its environment variable.
	"trade-satellite": "fixture-satellite-1", "intake:dots-p2": fixtureIntakeToken,
	// The collectors of two hosts (R5:5.5-12 §1.2).
	"collector:h1": "fixture-collector-h1", "collector:h2": "fixture-collector-h2"}

// recordMu orders the appends to cards: tests that run in parallel
// serve beside each other (those that read either run alone).
var recordMu sync.Mutex

// cards records the decision cards the API asked the Owner; askFails makes
// the next asks fail, as a full question store would; anchor is the key a
// signed rule must verify against.
var (
	cards    []planstore.Card
	askFails bool
	anchor   string
)

// server serves the API behind the platform's own authentication, loaded
// from a fixture file: two agentd tokens bound to h1 and h2, a viewer, a
// reporter, an operator, an admin, two seat tokens (one registered) and the
// collectors of h1 and h2.
func server(t *testing.T, db store.DB, with ...func(*agentrtapi.API)) *httptest.Server {
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
  - name: proposer
    role: operator
    token: fixture-proposer-1
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
  - name: seat:coordinator
    role: reporter
    token: fixture-seat-coordinator
  - name: agentd:coordinator-mbp
    role: agentd
    token: fixture-agentd-coordinator
  - name: trade-satellite
    role: operator
    token: fixture-satellite-1
  - name: collector:h1
    role: reporter
    token: fixture-collector-h1
  - name: collector:h2
    role: reporter
    token: fixture-collector-h2
intake_tokens:
  - name: intake:dots-p2
    token_env: `+fixtureIntakeEnv+`
`), 0o600))
	auth, err := actuation.LoadAuth(path)
	must(t, err)
	// A claim reads and parses the rules it acts on; under a whole suite's
	// load one pass can take longer than a few hundred milliseconds.
	api := &agentrtapi.API{DB: db, ClaimWait: time.Second, ClaimPoll: 50 * time.Millisecond,
		Ask: func(_ context.Context, c planstore.Card) (string, error) {
			if askFails {
				return "", fmt.Errorf("store full")
			}
			recordMu.Lock()
			defer recordMu.Unlock()
			cards = append(cards, c)
			return fmt.Sprintf("q_%d", len(cards)), nil
		}, Anchor: anchor}
	for _, f := range with {
		f(api)
	}
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

// With no database the routes answer 503 and nothing else happens.
func TestNotConfigured(t *testing.T) {
	s := server(t, nil)
	if code, out := call(t, s, "viewer", "GET", "progress", nil); code != http.StatusServiceUnavailable {
		t.Fatalf("%d %v", code, out)
	}
}

// Uploads: every kind but the four H8 lets through is scanned, whoever
// uploads it; a kind the schema does not have is refused (review 156 A2, B3).
func TestUploadKinds(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	leaky := []byte("input at /home/ci/private-input\n")
	for _, kind := range []string{"mission_spec", "message", "exam", "report"} {
		if code, _ := call(t, s, "proposer", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", kind, "X-Bifrost-Session", "s-rep"); code != http.StatusUnprocessableEntity {
			t.Errorf("a proposer's %s with a local path: %d", kind, code)
		}
	}
	for _, kind := range []string{"", "notes"} {
		if code, _ := call(t, s, "proposer", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", kind, "X-Bifrost-Session", "s-rep"); code != http.StatusBadRequest {
			t.Errorf("kind %q: %d", kind, code)
		}
	}
	if code, out := call(t, s, "proposer", "PUT", "artifacts/"+artifactID(leaky), leaky, "X-Artifact-Kind", "brief", "X-Bifrost-Session", "s-rep"); code != http.StatusOK || out["created"] != true {
		t.Errorf("a brief, which H8 lets through: %d %v", code, out)
	}
	var by string
	must(t, pool.QueryRow(ctx, `SELECT body_json->>'uploaded_by' FROM agentrt.event WHERE event_type = 'artifact_put'
	  AND body_json->>'artifact_id' = $1`, artifactID(leaky)).Scan(&by))
	if by != "proposer" {
		t.Errorf("uploaded_by = %q", by)
	}
}

// Review 157 B1: the same body uploaded twice at once is stored and
// recorded once.
func TestUploadRace(t *testing.T) {
	t.Parallel()
	pool, _ := world(t)
	s := server(t, pool)
	body := []byte("# race\n")
	created := make(chan any, 2)
	for _, who := range []string{"agentd:h1", "proposer"} {
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
