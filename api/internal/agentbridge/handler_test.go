package agentbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

// clearAgentBridgeEnv resets every optional bridge endpoint env var so tests
// start from a deterministic "not configured" baseline.
func clearAgentBridgeEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GIT_BRIDGE_URL", "SATELLITE_PROBE_BRIDGE_URL",
		"HERMES_GATEWAY_URL", "HERMES_MCP_URL",
		"NOUS_HERMES_URL", "NOUS_HERMES_USER", "NOUS_HERMES_PASS",
		"REMEDIATION_RUNNER_STANDBY_URL", "PLATFORM_PROJECT_ROOT",
	} {
		t.Setenv(k, "")
	}
}

func TestHandleBridgeNotConfiguredProbesReturnStatus(t *testing.T) {
	clearAgentBridgeEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","version":"1.0"}`))
	}))
	t.Cleanup(runner.Close)
	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)

	h := NewHandler()
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
	if resp.GitBridge.Status != "not_configured" {
		t.Fatalf("GitBridge.Status = %q, want not_configured", resp.GitBridge.Status)
	}
	if resp.SatelliteProbeBridge.Status != "not_configured" {
		t.Fatalf("SatelliteProbeBridge.Status = %q, want not_configured", resp.SatelliteProbeBridge.Status)
	}
	if resp.HermesMcp.Status != "not_configured" {
		t.Fatalf("HermesMcp.Status = %q, want not_configured", resp.HermesMcp.Status)
	}
	if resp.NousHermes.Status != "not_configured" {
		t.Fatalf("NousHermes.Status = %q, want not_configured", resp.NousHermes.Status)
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
}

func TestHandleBridgeAggregatesConfiguredProbes(t *testing.T) {
	clearAgentBridgeEnv(t)

	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok","version":"2.0"}`))
		case "/reports/latest":
			_, _ = w.Write([]byte(`{"content":"nightly summary","source":"nightly.sh","updated_at":"2026-07-01T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(runner.Close)

	gitBridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"workspace": "/stocks",
			"dirty_repos": ["bifrost-ui"],
			"repos": [{"repo":"bifrost-ui","branch":"main","dirty":true,"modified":["a.go"],"insertions":3,"deletions":1}]
		}`))
	}))
	t.Cleanup(gitBridge.Close)

	satelliteBridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"trade_nginx_base":"http://trade.local"}`))
	}))
	t.Cleanup(satelliteBridge.Close)

	hermesMcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"3.4"}`))
	}))
	t.Cleanup(hermesMcp.Close)

	nousHermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.1","gateway_running":true,"active_agents":2}`))
	}))
	t.Cleanup(nousHermes.Close)

	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)
	t.Setenv("GIT_BRIDGE_URL", gitBridge.URL)
	t.Setenv("SATELLITE_PROBE_BRIDGE_URL", satelliteBridge.URL)
	t.Setenv("HERMES_GATEWAY_URL", hermesMcp.URL)
	t.Setenv("NOUS_HERMES_URL", nousHermes.URL)

	h := NewHandler()
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
	if resp.GitBridge.Status != "ok" || resp.GitBridge.Workspace != "/stocks" || resp.GitBridge.DirtyRepos != 1 {
		t.Fatalf("GitBridge = %+v", resp.GitBridge)
	}
	if len(resp.GitBridge.DirtyRepoDetails) != 1 || resp.GitBridge.DirtyRepoDetails[0].Repo != "bifrost-ui" {
		t.Fatalf("GitBridge.DirtyRepoDetails = %+v", resp.GitBridge.DirtyRepoDetails)
	}
	if resp.SatelliteProbeBridge.Status != "ok" || resp.SatelliteProbeBridge.TradeNginxBase != "http://trade.local" {
		t.Fatalf("SatelliteProbeBridge = %+v", resp.SatelliteProbeBridge)
	}
	if resp.HermesMcp.Status != "ok" {
		t.Fatalf("HermesMcp = %+v", resp.HermesMcp)
	}
	if resp.NousHermes.Status != "ok" || resp.NousHermes.Version != "1.1" || !resp.NousHermes.GatewayRunning || resp.NousHermes.ActiveAgents != 2 {
		t.Fatalf("NousHermes = %+v", resp.NousHermes)
	}
	if !resp.NightlyReport.Available || resp.NightlyReport.Source != "nightly.sh" {
		t.Fatalf("NightlyReport = %+v", resp.NightlyReport)
	}
}

func TestHandleBridgeNousHermesAuthRequired(t *testing.T) {
	clearAgentBridgeEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(runner.Close)
	nousHermes := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(nousHermes.Close)

	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)
	t.Setenv("NOUS_HERMES_URL", nousHermes.URL)

	h := NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-bridge/bridge", nil)
	rec := httptest.NewRecorder()
	h.HandleBridge(rec, req)

	var resp BridgeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.NousHermes.Status != "auth_required" {
		t.Fatalf("NousHermes.Status = %q, want auth_required", resp.NousHermes.Status)
	}
}

func TestHandleSmokeProxiesRunnerResponse(t *testing.T) {
	clearAgentBridgeEnv(t)
	runner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/smoke" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(runner.Close)
	t.Setenv("REMEDIATION_RUNNER_URL", runner.URL)

	h := NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-bridge/smoke", nil)
	rec := httptest.NewRecorder()
	h.HandleSmoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestHandleSmokeNoRunnerConfigured(t *testing.T) {
	h := &Handler{runner: &remediation.RunnerClient{}, httpClient: &http.Client{}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-bridge/smoke", nil)
	rec := httptest.NewRecorder()
	h.HandleSmoke(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
	}
}
