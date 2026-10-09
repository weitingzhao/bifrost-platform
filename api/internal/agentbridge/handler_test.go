package agentbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// clearAgentBridgeEnv resets every optional bridge endpoint env var so tests
// start from a deterministic "not configured" baseline.
func clearAgentBridgeEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GIT_BRIDGE_URL", "SATELLITE_PROBE_BRIDGE_URL",
		"REMEDIATION_RUNNER_URL", "REMEDIATION_RUNNER_STANDBY_URL",
		"PLANE_HEALTH_URLS", "PLATFORM_PROJECT_ROOT",
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
	t.Setenv("PLANE_HEALTH_URLS", runner.URL)

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
	if resp.GitBridge.Error != localOnlyBridgeText {
		t.Fatalf("GitBridge.Error = %q, want %q", resp.GitBridge.Error, localOnlyBridgeText)
	}
	if resp.SatelliteProbeBridge.Status != "not_configured" {
		t.Fatalf("SatelliteProbeBridge.Status = %q, want not_configured", resp.SatelliteProbeBridge.Status)
	}
	if resp.SatelliteProbeBridge.Error != localOnlyBridgeText {
		t.Fatalf("SatelliteProbeBridge.Error = %q, want %q", resp.SatelliteProbeBridge.Error, localOnlyBridgeText)
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

	t.Setenv("PLANE_HEALTH_URLS", runner.URL)
	t.Setenv("GIT_BRIDGE_URL", gitBridge.URL)
	t.Setenv("SATELLITE_PROBE_BRIDGE_URL", satelliteBridge.URL)

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
	if !resp.NightlyReport.Available {
		if resp.NightlyReport.Hint == "" {
			t.Fatalf("NightlyReport = %+v, want a hint now that the runner is gone", resp.NightlyReport)
		}
	} else {
		t.Fatalf("NightlyReport = %+v, want unavailable", resp.NightlyReport)
	}
}

func TestProbeGitBridgeSendsOperatorToken(t *testing.T) {
	const token = "fixture-operator-token"
	var got string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"workspace":"/stocks","repos":[]}`))
	}))
	t.Cleanup(gw.Close)
	t.Setenv("GIT_BRIDGE_URL", gw.URL)
	t.Setenv("PLATFORM_OPERATOR_TOKEN", token)
	t.Setenv("PLATFORM_ADMIN_TOKEN", "")

	status := probeGitBridge(context.Background(), gw.Client())
	if status.Status != "ok" {
		t.Fatalf("probeGitBridge status = %q, want ok", status.Status)
	}
	if got != "Bearer "+token {
		t.Fatal("git-bridge probe did not send the operator bearer")
	}
}

func TestProbeGitBridgeUsesProdOperatorToken(t *testing.T) {
	const token = "fixture-prod-operator-token"
	var got string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"workspace":"/stocks","repos":[]}`))
	}))
	t.Cleanup(gw.Close)
	t.Setenv("GIT_BRIDGE_URL", gw.URL)
	t.Setenv("PLATFORM_OPERATOR_TOKEN", "")
	t.Setenv("PLATFORM_ADMIN_TOKEN", "")
	t.Setenv("PLATFORM_PROD_OPERATOR_TOKEN", token)
	t.Setenv("PLATFORM_PROD_ADMIN_TOKEN", "")

	status := probeGitBridge(context.Background(), gw.Client())
	if status.Status != "ok" {
		t.Fatalf("probeGitBridge status = %q, want ok", status.Status)
	}
	if got != "Bearer "+token {
		t.Fatal("git-bridge probe did not send the prod operator bearer")
	}
}
