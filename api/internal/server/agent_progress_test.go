package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentprogress"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

const agentProgressAuthYAML = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-w62
  - name: reporter
    role: reporter
    token: fixture-reporter-w62
`

func progressConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	t.Setenv("PLATFORM_AGENT_THREAD_WATCH", "off")
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(agentProgressAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg, os.Getenv("PLATFORM_DATA_DIR")
}

func progressServer(t *testing.T) *Server {
	t.Helper()
	cfg, _ := progressConfig(t)
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	return srv
}

func progressDo(srv *Server, method, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/agent/progress", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestAgentProgressRoles(t *testing.T) {
	srv := progressServer(t)
	doc := `{"at":1,"verdict":"in progress","host":"vision-mac"}`

	if rec := progressDo(srv, http.MethodPut, "", doc); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous put: got %d, want 401", rec.Code)
	}
	if rec := progressDo(srv, http.MethodGet, "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous get: got %d, want 401", rec.Code)
	}
	if rec := progressDo(srv, http.MethodGet, "not-a-token", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token get: got %d, want 401", rec.Code)
	}
	empty := progressDo(srv, http.MethodGet, "fixture-viewer-w62", "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"document":null`) {
		t.Fatalf("nothing published: %d %s", empty.Code, empty.Body.String())
	}
	if rec := progressDo(srv, http.MethodPut, "fixture-viewer-w62", doc); rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer put: got %d, want 401: %s", rec.Code, rec.Body.String())
	}
	posted := progressDo(srv, http.MethodPut, "fixture-reporter-w62", doc)
	if posted.Code != http.StatusAccepted {
		t.Fatalf("reporter put: got %d, want 202: %s", posted.Code, posted.Body.String())
	}
	got := progressDo(srv, http.MethodGet, "fixture-viewer-w62", "")
	if got.Code != http.StatusOK {
		t.Fatalf("viewer get: got %d, want 200: %s", got.Code, got.Body.String())
	}
	var body struct {
		Document json.RawMessage `json:"document"`
		Host     string          `json:"host"`
		Stale    bool            `json:"stale"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Stale || body.Host != "vision-mac" || !strings.Contains(string(body.Document), `"verdict":"in progress"`) {
		t.Fatalf("viewer body: %s", got.Body.String())
	}
	if rec := progressDo(srv, http.MethodGet, "fixture-reporter-w62", ""); rec.Code != http.StatusOK {
		t.Fatalf("reporter get: got %d, want 200", rec.Code)
	}
}

func TestAgentProgressMalformedSizeRedactionLatest(t *testing.T) {
	srv := progressServer(t)
	secret := "Bearer abcdefghijklmnop"
	if rec := progressDo(srv, http.MethodPut, "fixture-reporter-w62", `not-json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed: got %d, want 400", rec.Code)
	}
	if rec := progressDo(srv, http.MethodPut, "fixture-reporter-w62", `{"verdict":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing at: got %d, want 400", rec.Code)
	}
	over := `{"at":1,"v":"` + strings.Repeat("a", int(agentprogress.MaxDocumentBytes)) + `"}`
	if rec := progressDo(srv, http.MethodPut, "fixture-reporter-w62", over); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("size: got %d, want 413", rec.Code)
	}
	if rec := progressDo(srv, http.MethodPut, "fixture-reporter-w62", `{"at":1,"verdict":"first"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	nested := `{"at":10,"host":"mini","works":[{"note":"token ` + secret + `"}]}`
	if rec := progressDo(srv, http.MethodPut, "fixture-reporter-w62", nested); rec.Code != http.StatusAccepted {
		t.Fatalf("second: %d %s", rec.Code, rec.Body.String())
	}
	got := progressDo(srv, http.MethodGet, "fixture-viewer-w62", "")
	if strings.Contains(got.Body.String(), "abcdefghijklmnop") || !strings.Contains(got.Body.String(), "Bearer [redacted]") {
		t.Fatalf("redaction: %s", got.Body.String())
	}
	if strings.Contains(got.Body.String(), `"verdict":"first"`) || !strings.Contains(got.Body.String(), `"host":"mini"`) {
		t.Fatalf("latest-wins: %s", got.Body.String())
	}
}

func TestAgentProgressSurvivesRestartAndStale(t *testing.T) {
	cfg, dataDir := progressConfig(t)
	path := filepath.Join(dataDir, agentprogress.StateRel)
	rec := agentprogress.NewRecorder(agentprogress.NewStore(path))
	rec.Publish(json.RawMessage(`{"at":3,"verdict":"kept","extra":"仍在"}`), "mini")
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := progressDo(srv, http.MethodGet, "fixture-viewer-w62", "")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"kept"`) || !strings.Contains(got.Body.String(), "仍在") {
		t.Fatalf("restart: %d %s", got.Code, got.Body.String())
	}
	if strings.Contains(got.Body.String(), `"stale":true`) {
		t.Fatalf("fresh document marked stale: %s", got.Body.String())
	}

	old := time.Now().UTC().Add(-6 * time.Minute).Format(time.RFC3339Nano)
	raw := []byte(`{"document":{"at":4,"verdict":"held"},"received_at":"` + old + `","host":"mini"}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	srv2, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stale := progressDo(srv2, http.MethodGet, "fixture-viewer-w62", "")
	if stale.Code != http.StatusOK || !strings.Contains(stale.Body.String(), `"stale":true`) || !strings.Contains(stale.Body.String(), `"held"`) {
		t.Fatalf("stale after restart: %d %s", stale.Code, stale.Body.String())
	}
}
