package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/stgrelease"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// TD-460: the sample actions on the real approval path. A malformed request
// is refused before the row exists; a Tier C request waits for the Console;
// the executor never runs without an approval; and an approved create whose
// commit has no accepted release CI fails without allocating a sample.
func TestSTGSampleApprovalOnTheRealPath(t *testing.T) {
	ctx := context.Background()
	cfg := newTestConfig(t)
	authFile := filepath.Join(t.TempDir(), "platform-auth.yaml")
	if err := os.WriteFile(authFile, []byte("tokens:\n  - name: viewer\n    role: viewer\n    token: viewer-test-token\n"+
		"  - name: operator\n    role: operator\n    token: operator-test-token\n  - name: admin\n    role: admin\n    token: admin-test-token\n"), 0o600); err != nil {
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
	conn := db.Connect(t, db.Runtime)
	srv.agentRuntime.api.DB = conn
	h := srv.Router()
	do := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	sha := func(c string) string { return strings.Repeat(c, 40) }
	request := func(action string, params map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"action": action, "reason": "TD-460 first-opening sample", "rollback": "finalize the sample", "params": params})
		return do(http.MethodPost, "/api/v1/approvals", string(body), "operator-test-token")
	}
	for _, bad := range []struct {
		action string
		params map[string]any
	}{
		{stgrelease.SampleCreateAction, map[string]any{"env": "prod", "revision": sha("a"), "ui_revision": sha("b"), "infra_revision": sha("c")}},
		{stgrelease.SampleCreateAction, map[string]any{"env": "stg", "revision": "main", "ui_revision": sha("b"), "infra_revision": sha("c")}},
		{stgrelease.SampleFinalizeAction, map[string]any{"env": "dev", "run": "stgrel-1-1-aaaaaaaa", "uid": "00000000-0000-4000-8000-000000000001"}},
		{stgrelease.SampleFinalizeAction, map[string]any{"env": "stg", "run": "stgrel-1-2-aaaaaaaa", "uid": "00000000-0000-4000-8000-000000000001"}},
		{stgrelease.SampleFinalizeAction, map[string]any{"env": "stg", "run": "stgrel-1-1-aaaaaaaa", "uid": "fresh-get-uid"}},
	} {
		if rec := request(bad.action, bad.params); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%v: %d %s", bad.params, rec.Code, rec.Body.String())
		}
	}
	rec := request(stgrelease.SampleCreateAction, map[string]any{"env": "stg", "revision": sha("a"), "ui_revision": sha("b"), "infra_revision": sha("c")})
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"tier":"C"`) {
		t.Fatalf("request = %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if _, err := actions.Execute(ctx, stgrelease.SampleCreateAction, map[string]any{"env": "stg", "revision": sha("a"), "ui_revision": sha("b"), "infra_revision": sha("c")}); err == nil {
		t.Fatal("the executor ran without an approval")
	}
	get := do(http.MethodGet, "/api/v1/approvals/"+created.ID, "", "viewer-test-token")
	var row struct {
		ApprovalLine string `json:"approval_line"`
		ParamsHash   string `json:"params_hash"`
		Number       int    `json:"number"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &row); err != nil || row.ApprovalLine == "" {
		t.Fatalf("get = %d %s", get.Code, get.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"channel": "console", "approval_line": row.ApprovalLine, "params_hash": row.ParamsHash, "confirm_number": row.Number})
	out := do(http.MethodPost, "/api/v1/approvals/"+created.ID+"/approve", string(body), "admin-test-token").Body.String()
	if !strings.Contains(out, `"status":"failed"`) {
		t.Fatalf("approve without an accepted CI or cluster = %s", out)
	}
	s, err := store.ReadStgSamples(ctx, conn)
	if err != nil || s.LastSeq != 0 {
		t.Fatalf("a failed create allocated a sample: %+v %v", s, err)
	}
}

func TestSTGSampleErrorClasses(t *testing.T) {
	t.Parallel()
	if !actions.IsTransient(stgSampleError(errors.Join(stgrelease.ErrSampleBusy))) {
		t.Fatal("busy window is transient")
	}
	if !actions.IsUncertain(stgSampleError(errors.Join(stgrelease.ErrSampleUncertain))) {
		t.Fatal("unknown create is uncertain")
	}
	if err := stgSampleError(errors.New("refused")); actions.IsTransient(err) || actions.IsUncertain(err) {
		t.Fatal("a refusal stays a failure")
	}
}
