package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

const alertmanagerAuthYAML = `
tokens:
  - name: viewer
    role: viewer
    token: fixture-viewer-b4
  - name: reporter
    role: reporter
    token: fixture-reporter-b4
  - name: operator
    role: operator
    token: fixture-operator-b4
`

func TestAlertmanagerWebhookIsReporterOrAbove(t *testing.T) {
	t.Setenv("PLATFORM_DATA_CLONE_SCHEDULER", "off")
	t.Setenv("PLATFORM_PATROL_LOOP", "off")
	cfg := newTestConfig(t)
	if err := os.WriteFile(cfg.PlatformAuthPath, []byte(alertmanagerAuthYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	body := `{"receiver":"bifrost-ops-agent","status":"firing","alerts":[{"status":"firing","labels":{"alertname":"Watchdog"}}]}`
	post := func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/ops-agent/alertmanager", strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: got %d, want 401", code)
	}
	if code := post("fixture-viewer-b4"); code != http.StatusUnauthorized {
		t.Fatalf("viewer: got %d, want 401", code)
	}
	if code := post("fixture-reporter-b4"); code != http.StatusOK {
		t.Fatalf("reporter: got %d, want 200", code)
	}
	if code := post("fixture-operator-b4"); code != http.StatusOK {
		t.Fatalf("operator: got %d, want 200", code)
	}
}
