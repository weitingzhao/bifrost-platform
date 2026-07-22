package agentreport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// clearAgentReportEnv resets every env var NewHandler reads so tests start
// from a deterministic baseline.
func clearAgentReportEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"REMEDIATION_RUNNER_URL", "AGENT_NIGHTLY_REPORT_PATH", "BIFROST_PLATFORM_ROOT"} {
		t.Setenv(k, "")
	}
}

func TestHandleNightlyReportNoRunnerNoLocalFile(t *testing.T) {
	clearAgentReportEnv(t)
	// Point AGENT_NIGHTLY_REPORT_PATH at a file that does not exist so the
	// local-file fallback also fails deterministically.
	t.Setenv("AGENT_NIGHTLY_REPORT_PATH", filepath.Join(t.TempDir(), "does-not-exist.md"))
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-report/nightly", nil)
	rec := httptest.NewRecorder()
	h.HandleNightlyReport(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp NightlyReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Available {
		t.Fatalf("resp.Available = true, want false: %+v", resp)
	}
	if resp.Hint == "" {
		t.Fatal("expected a hint when no report is available")
	}
}

func TestHandleNightlyReportReadsLocalFile(t *testing.T) {
	clearAgentReportEnv(t)
	path := filepath.Join(t.TempDir(), "latest.md")
	if err := os.WriteFile(path, []byte("# Nightly report\nAll clear."), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_NIGHTLY_REPORT_PATH", path)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-report/nightly", nil)
	rec := httptest.NewRecorder()
	h.HandleNightlyReport(rec, req)

	var resp NightlyReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Available || resp.Content != "# Nightly report\nAll clear." || resp.Source != path {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandleNightlyReportPrefersRunnerOverLocalFile(t *testing.T) {
	clearAgentReportEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reports/latest" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"content":"from runner","source":"remediation-runner","updated_at":"2026-07-01T00:00:00Z"}`))
	}))
	t.Cleanup(runner.Close)
	// Also set up a local file so we can assert the runner takes priority.
	path := filepath.Join(t.TempDir(), "latest.md")
	if err := os.WriteFile(path, []byte("local content"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_NIGHTLY_REPORT_PATH", path)
	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-report/nightly", nil)
	rec := httptest.NewRecorder()
	h.HandleNightlyReport(rec, req)

	var resp NightlyReportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Available || resp.Content != "from runner" || resp.GeneratedAt != "2026-07-01T00:00:00Z" {
		t.Fatalf("resp = %+v, want runner content to take priority", resp)
	}
}

func TestHandleTriggerNightlyNoRunnerConfigured(t *testing.T) {
	clearAgentReportEnv(t)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-report/nightly/trigger", nil)
	rec := httptest.NewRecorder()
	h.HandleTriggerNightly(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleTriggerNightlySuccess(t *testing.T) {
	clearAgentReportEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/nightly/run" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"started","script":"nightly.sh"}`))
	}))
	t.Cleanup(runner.Close)
	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-report/nightly/trigger", nil)
	rec := httptest.NewRecorder()
	h.HandleTriggerNightly(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body=%s", rec.Code, rec.Body.String())
	}
	var resp NightlyTriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "started" || resp.Script != "nightly.sh" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestHandleTriggerNightlyPropagatesUpstreamError(t *testing.T) {
	clearAgentReportEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	t.Cleanup(runner.Close)
	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)
	h := NewHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent-report/nightly/trigger", nil)
	rec := httptest.NewRecorder()
	h.HandleTriggerNightly(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (proxied)", rec.Code)
	}
}
