package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const agentThreadsAuthYAML = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-w54
  - name: reporter
    role: reporter
    token: fixture-reporter-w54
`

// Thread titles and host names are not open: the list is viewer and above,
// and only a reporter (or above) may post a heartbeat.
func TestAgentThreadsRoutesAuth(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	t.Setenv("PLATFORM_AGENT_THREAD_WATCH", "off")
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(agentThreadsAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	do := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodGet, "/api/v1/agent/threads", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list: got %d, want 401", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/agent/threads", "not-a-token", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown token list: got %d, want 401", rec.Code)
	}

	beat := `{"thread":"0b8e2f4a-1111-2222-3333-444455556666","vendor":"claude","host":"vision-mac","event":"turn_start"}`
	if rec := do(http.MethodPost, "/api/v1/agent/threads/heartbeat", "fixture-viewer-w54", beat); rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer heartbeat: got %d, want 401", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/v1/agent/threads/heartbeat", "fixture-reporter-w54", beat); rec.Code != http.StatusAccepted {
		t.Fatalf("reporter heartbeat: got %d, want 202: %s", rec.Code, rec.Body.String())
	}

	rec := do(http.MethodGet, "/api/v1/agent/threads", "fixture-viewer-w54", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer list: got %d, want 200", rec.Code)
	}
	var body struct {
		Threads []struct {
			Thread string `json:"thread"`
			Status string `json:"status"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(body.Threads) != 1 || body.Threads[0].Status != "in_turn" {
		t.Fatalf("viewer list: got %+v, want the one thread in turn", body.Threads)
	}
	if rec := do(http.MethodGet, "/api/v1/agent/threads", "fixture-reporter-w54", ""); rec.Code != http.StatusOK {
		t.Fatalf("reporter list: got %d, want 200", rec.Code)
	}
}
