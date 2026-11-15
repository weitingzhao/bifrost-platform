package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/migrate"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Origin: the Owner's redefinition of the card (2026-11-15). A direct call of
// a catalogued action is recorded when it happens: this test makes a real
// call through the real router and reads the record back by seat, which is
// how the Owner will read it. It also keeps the first-phase promise that the
// record never changes what the action answers.

func TestADirectActionIsRecordedAndReadBackBySeat(t *testing.T) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n"+
		"  - name: operator\n    role: operator\n    token: operator-test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = authFile
	t.Setenv("AGENTRT_ENV", "prod")
	t.Setenv("AGENTRT_DSN", "")
	srv := newTestServer(t, cfg)
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	srv.agentRuntime.api.DB = db.Pool(t, db.Runtime)
	h := srv.Router()
	do := func(method, path, body, token, session string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		if session != "" {
			req.Header.Set("X-Bifrost-Session", session)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	// record_release_review is a tier B action that needs only the database.
	sha := strings.Repeat("a", 40)
	body := `{"repo":"bifrost-platform","commit":"` + sha + `","files":["api/x.go"],"verdict":"passed"}`
	if rec := do(http.MethodPost, "/api/v1/release-policy/reviews", body, "operator-test-token", "local_seat_a"); rec.Code != http.StatusOK {
		t.Fatalf("the action = %d %s", rec.Code, rec.Body.String())
	}
	// A refused call (a bad body the action refuses) is recorded as failed, with its seat.
	do(http.MethodPost, "/api/v1/release-policy/reviews", `{"repo":"bifrost-platform","commit":"nope","files":["x"],"verdict":"passed"}`, "operator-test-token", "local_seat_b")

	var out struct {
		Records []map[string]any `json:"records"`
		More    bool             `json:"more"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		rec := do(http.MethodGet, "/api/v1/records?seat=local_seat_a", "", "viewer-test-token", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("read = %d %s", rec.Code, rec.Body.String())
		}
		out.Records = nil
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if len(out.Records) == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(out.Records) != 1 {
		t.Fatalf("records of seat a = %d, want 1", len(out.Records))
	}
	r := out.Records[0]
	if r["action"] != "record_release_review" || r["outcome"] != "done" || r["seat"] != "local_seat_a" ||
		r["regret_state"] != "undeclared" || r["law"] != "action catalog: tier B" || r["reason"] == "" {
		t.Fatalf("record = %v", r)
	}
	// The other seat's failed call is a record of its own.
	for time.Now().Before(deadline) {
		rec := do(http.MethodGet, "/api/v1/records?seat=local_seat_b&action=record_release_review", "", "viewer-test-token", "")
		out.Records = nil
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if len(out.Records) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(out.Records) != 1 || out.Records[0]["outcome"] != "failed" {
		t.Fatalf("records of seat b = %v", out.Records)
	}
	// The read refuses a bad filter instead of ignoring it.
	if rec := do(http.MethodGet, "/api/v1/records?since=yesterday", "", "viewer-test-token", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a bad since = %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/records?limit=5000", "", "viewer-test-token", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("a bad limit = %d", rec.Code)
	}
}

// The record is built from what the caller sent and nothing else: a call that
// gave a reason, a rollback and an unchecked list carries them; one that gave
// none says so rather than making them up.
func TestDirectRecordSaysWhatTheCallerLeftOut(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("X-Bifrost-Session", "local_s")
	act := mustAction(t, "rollout_restart_deployment")
	bare := directRecord(context.Background(), req, act, map[string]any{"namespace": "cicd", "kind": "Deployment", "name": "x"}, "action catalog: tier B", 200)
	if bare.Reason != noReason || bare.RegretState != "undeclared" || bare.Outcome != "done" || bare.Seat != "local_s" {
		t.Fatalf("bare = %+v", bare)
	}
	full := directRecord(context.Background(), req, act, map[string]any{"namespace": "cicd", "kind": "Deployment", "name": "x",
		"reason": "reload cert", "rollback": "restart again", "unchecked": "none"}, "action catalog: tier B", 403)
	if full.Reason != "reload cert" || full.RegretState != "declared" || full.Regret != "restart again" || full.NotVerified != "none" || full.Outcome != "refused" {
		t.Fatalf("full = %+v", full)
	}
	if got := directRecord(context.Background(), req, act, map[string]any{}, "l", 500); got.Outcome != "failed" {
		t.Fatalf("a 500 = %s", got.Outcome)
	}
}

// A database without 0022 (the platform ran before the migration): the action
// answers as it did, and the missing record is logged, not returned.
func TestAnActionIsUnaffectedWhenTheRecordTableIsMissing(t *testing.T) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: operator\n    role: operator\n    token: operator-test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = authFile
	t.Setenv("AGENTRT_ENV", "prod")
	t.Setenv("AGENTRT_DSN", "")
	srv := newTestServer(t, cfg)
	db := agentrttest.New(t)
	conn := db.Connect(t, db.Owner)
	files, err := migrate.Files()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = migrate.RunFiles(ctx, conn, "dev", files[:21]); err != nil {
		t.Fatal(err)
	}
	srv.agentRuntime.api.DB = db.Pool(t, db.Runtime)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-policy/reviews",
		strings.NewReader(`{"repo":"bifrost-platform","commit":"`+strings.Repeat("a", 40)+`","files":["api/x.go"],"verdict":"passed"}`))
	req.Header.Set("Authorization", "Bearer operator-test-token")
	req.Header.Set("X-Bifrost-Session", "local_seat_a")
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("the action without the record table = %d %s", rec.Code, rec.Body.String())
	}
}
