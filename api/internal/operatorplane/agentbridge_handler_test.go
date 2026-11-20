package operatorplane

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// clearAgentBridgeEnv resets the runner env vars so tests start from a
// deterministic baseline.
func clearAgentBridgeEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"REMEDIATION_RUNNER_URL", "REMEDIATION_RUNNER_STANDBY_URL",
		"PLANE_HEALTH_URLS", "PLATFORM_PROJECT_ROOT",
	} {
		t.Setenv(k, "")
	}
}

func TestHandleBridgeReturnsRunnerAndPlatformMcp(t *testing.T) {
	clearAgentBridgeEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"1.0"}`))
	}))
	t.Cleanup(runner.Close)
	t.Setenv("PLANE_HEALTH_URLS", runner.URL)

	h := NewAgentBridgeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-bridge/bridge", nil)
	rec := httptest.NewRecorder()
	h.HandleBridge(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp BridgeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.PlatformMcp.ServerName == "" || resp.PlatformMcp.ToolCount == 0 {
		t.Fatalf("PlatformMcp = %+v, want populated catalog stats", resp.PlatformMcp)
	}
	if resp.RemediationRunner.Status != "ok" {
		t.Fatalf("RemediationRunner.Status = %q, want ok", resp.RemediationRunner.Status)
	}
	if len(resp.Runners) != 1 {
		t.Fatalf("Runners = %+v, want 1 entry", resp.Runners)
	}

	// 2026-11-18 (R12): PROD could not fill these fields (git-bridge and
	// probe-bridge unreachable from PROD, Hermes and the nightly report gone);
	// they were removed. The response must not bring them back.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, k := range []string{"git_bridge", "satellite_probe_bridge", "hermes_mcp", "nous_hermes", "nightly_report"} {
		if _, ok := raw[k]; ok {
			t.Fatalf("response still carries %q", k)
		}
	}
	for _, k := range []string{"generated_at", "remediation_runner", "platform_mcp"} {
		if _, ok := raw[k]; !ok {
			t.Fatalf("response lost %q", k)
		}
	}
}
