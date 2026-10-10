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

	beat := `{"thread":"0b8e2f4a-1111-2222-3333-444455556666","vendor":"claude","host":"vision-mac","event":"turn_start","turn_id":"turn-1","seq":1}`
	if rec := do(http.MethodPost, "/api/v1/agent/threads/heartbeat", "fixture-viewer-w54", beat); rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer heartbeat: got %d, want 401", rec.Code)
	}
	posted := do(http.MethodPost, "/api/v1/agent/threads/heartbeat", "fixture-reporter-w54", beat)
	if posted.Code != http.StatusAccepted {
		t.Fatalf("reporter heartbeat: got %d, want 202: %s", posted.Code, posted.Body.String())
	}
	var issued struct {
		Key string `json:"thread_key"`
	}
	if err := json.Unmarshal(posted.Body.Bytes(), &issued); err != nil || len(issued.Key) != 64 {
		t.Fatalf("first event did not issue a key: %s", posted.Body.String())
	}

	hostBeat := `{"host":"vision-mac","vendors":{"claude":{"wired":true,"token":true},"cursor":{"wired":false,"token":false},"codex":{"wired":true,"token":false}}}`
	if rec := do(http.MethodPost, "/api/v1/agent/hosts/heartbeat", "fixture-viewer-w54", hostBeat); rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer host heartbeat: got %d, want 401", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/v1/agent/hosts/heartbeat", "fixture-reporter-w54", hostBeat); rec.Code != http.StatusAccepted {
		t.Fatalf("reporter host heartbeat: got %d, want 202: %s", rec.Code, rec.Body.String())
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
	if strings.Contains(rec.Body.String(), issued.Key) || strings.Contains(rec.Body.String(), "key_hash") || strings.Contains(rec.Body.String(), "thread_key") {
		t.Fatalf("list returned a thread key or its hash: %s", rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/v1/agent/threads", "fixture-reporter-w54", ""); rec.Code != http.StatusOK {
		t.Fatalf("reporter list: got %d, want 200", rec.Code)
	}
}
