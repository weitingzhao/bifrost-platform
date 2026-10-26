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

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Exercise the same API request_action calls, including the Console record,
// admin decision, executor context, runtime role and event chain.
func TestMarkMissionsNoMergeApprovalPath(t *testing.T) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n"+
		"  - name: operator\n    role: operator\n    token: operator-test-token\n  - name: admin\n    role: admin\n    token: admin-test-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.PlatformAuthPath = authFile
	t.Setenv("AGENTRT_ENV", "stg")
	t.Setenv("AGENTRT_DSN", "")
	srv := newTestServer(t, cfg)
	db := agentrttest.Migrated(t)
	if _, err := store.Seed(ctx, db.Connect(t, db.Owner)); err != nil {
		t.Fatal(err)
	}
	conn := db.Connect(t, db.Runtime)
	srv.agentRuntime.api.DB = conn
	if err := store.InTx(ctx, conn, func(tx pgx.Tx) error {
		_, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: "M-1", Title: "archived drill", Goal: "test", Deadline: time.Now().Add(time.Hour)})
		if err != nil {
			return err
		}
		if err = store.MoveMission(ctx, tx, "controller", "M-1", "active", "test"); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", "M-1", "done", "test")
	}); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := conn.QueryRow(ctx, `SELECT version FROM agentrt.mission WHERE mission_id='M-1'`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	params := func(env string) map[string]any {
		return map[string]any{"env": env, "missions": []any{map[string]any{"mission_id": "M-1", "expected_version": version}},
			"reason": "archived drill", "archive_manifest_sha256": strings.Repeat("ab", 32)}
	}
	h := srv.Router()
	do := func(method, path string, body any, role string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+role+"-test-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	request := func(p map[string]any) *httptest.ResponseRecorder {
		return do(http.MethodPost, "/api/v1/approvals", map[string]any{"action": "mark_missions_no_merge", "params": p, "reason": "D-L5 archive verified", "rollback": "separate approved correction required"}, "operator")
	}
	if _, err := actions.Execute(ctx, "mark_missions_no_merge", params("stg")); err == nil {
		t.Fatal("ran without approval")
	}
	// Refuse the wrong environment before filing and again at execution.
	if rec := request(params("prod")); rec.Code != 422 {
		t.Fatalf("wrong env: %d %s", rec.Code, rec.Body.String())
	}
	execCtx := actions.WithCreateAttempt(ctx, "appr_0123456789abcdef", 1)
	if _, err := srv.markMissionsNoMerge(execCtx, params("prod")); err == nil {
		t.Fatal("cross-environment execution")
	}
	for _, missions := range []any{nil, "M-1", []any{}, []any{map[string]any{"mission_id": "M-1", "expected_version": 1.5}}, []any{map[string]any{"mission_id": "M-1", "expected_version": version, "extra": "silently lost?"}}} {
		p := params("stg")
		p["missions"] = missions
		if rec := request(p); rec.Code != 400 && rec.Code != 422 {
			t.Fatalf("invalid missions accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := request(params("stg"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	rec = do(http.MethodGet, "/api/v1/approvals/"+created.ID, nil, "viewer")
	var row struct {
		Tier         string               `json:"tier"`
		Env          string               `json:"env"`
		Runner       string               `json:"runner"`
		Summary      string               `json:"summary"`
		Params       store.NoMergeRequest `json:"params"`
		ApprovalLine string               `json:"approval_line"`
		ParamsHash   string               `json:"params_hash"`
		Number       int                  `json:"number"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row.Tier != "D" || row.Env != "stg" || row.Runner != "platform" || !strings.Contains(row.Summary, "no_merge") || row.Params.Validate() != nil || row.Params.Missions[0].ExpectedVersion != version {
		t.Fatalf("Console record: %s", rec.Body.String())
	}
	decision := map[string]any{"channel": "console", "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash, "confirm_number": row.Number}
	if rec = do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", decision, "operator"); rec.Code != http.StatusUnauthorized {
		t.Fatal("operator approved", rec.Body.String())
	}
	if rec = do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", map[string]any{"channel": "chat"}, "admin"); rec.Code != 403 {
		t.Fatal("chat approved", rec.Body.String())
	}
	rec = do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", decision, "admin")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"executed"`) {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	var got, reason string
	var newVersion int64
	if err := conn.QueryRow(ctx, `SELECT scope_json->>'no_merge',version FROM agentrt.mission WHERE mission_id='M-1'`).Scan(&reason, &newVersion); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `SELECT body_json->>'approval_id' FROM agentrt.event WHERE mission_id='M-1' AND event_type='mission_amended'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != created.ID || newVersion != version+1 || reason != "archived drill" {
		t.Fatal("wrong persisted result", got, newVersion, reason)
	}
	if _, err := store.VerifyStream(ctx, conn, store.MissionStream("M-1")); err != nil {
		t.Fatal(err)
	}
}
