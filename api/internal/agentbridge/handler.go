package agentbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

// Handler aggregates autonomous agent host + MCP bridge status for Console.
type Handler struct {
	runner     *remediation.RunnerClient
	httpClient *http.Client
}

func NewHandler() *Handler {
	return &Handler{
		runner:     remediation.NewRunnerClient(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type BridgeResponse struct {
	GeneratedAt       time.Time         `json:"generated_at"`
	RemediationRunner RunnerStatus      `json:"remediation_runner"`
	Runners           []RunnerStatus    `json:"runners"`
	GitBridge         GitBridgeStatus   `json:"git_bridge"`
	HermesMcp         OptionalEndpoint  `json:"hermes_mcp"`
	NousHermes        NousHermesStatus  `json:"nous_hermes"`
	PlatformMcp       PlatformMcpStatus `json:"platform_mcp"`
	NightlyReport     NightlyHint       `json:"nightly_report"`
}

type NousHermesStatus struct {
	URL             string `json:"url,omitempty"`
	Status          string `json:"status"`
	Version         string `json:"version,omitempty"`
	ReleaseDate     string `json:"release_date,omitempty"`
	GatewayRunning  bool   `json:"gateway_running"`
	GatewayState    string `json:"gateway_state,omitempty"`
	ActiveAgents    int    `json:"active_agents"`
	ActiveSessions  int    `json:"active_sessions"`
	McpToolCount    int    `json:"mcp_tool_count"`
	DashboardURL    string `json:"dashboard_url,omitempty"`
	Error           string `json:"error,omitempty"`
}

type GitBridgeStatus struct {
	URL        string `json:"url,omitempty"`
	Status     string `json:"status"` // not_configured | ok | unavailable
	Workspace  string `json:"workspace,omitempty"`
	RepoCount  int    `json:"repo_count,omitempty"`
	DirtyRepos int    `json:"dirty_repos,omitempty"`
	Error      string `json:"error,omitempty"`
}

type RunnerStatus struct {
	URL          string `json:"url"`
	Role         string `json:"role,omitempty"` // primary | standby
	Status       string `json:"status"`
	Version      string `json:"version,omitempty"`
	Active       bool   `json:"active,omitempty"`
	CursorAPIKey bool   `json:"cursor_api_key,omitempty"`
	Service      string `json:"service,omitempty"`
	Error        string `json:"error,omitempty"`
}

type OptionalEndpoint struct {
	URL       string `json:"url,omitempty"`
	Status    string `json:"status"` // not_configured | ok | unavailable
	Error     string `json:"error,omitempty"`
	Note      string `json:"note,omitempty"`
}

type PlatformMcpStatus struct {
	ServerName       string `json:"server_name"`
	ServerVersion    string `json:"server_version"`
	ToolCount        int    `json:"tool_count"`
	ImplementedCount int    `json:"implemented_count"`
	AgentToolCount   int    `json:"agent_tool_count"`
	Transport        string `json:"transport"`
	ScriptPath       string `json:"script_path"`
}

type NightlyHint struct {
	Available   bool   `json:"available"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Source      string `json:"source,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

func (h *Handler) HandleBridge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()

	healths := h.runner.HealthAll(ctx)
	runners := make([]RunnerStatus, 0, len(healths))
	var primary RunnerStatus
	var active RunnerStatus
	for _, hh := range healths {
		rs := RunnerStatus{
			URL:          hh.URL,
			Role:         hh.Role,
			Status:       hh.Status,
			Version:      hh.Version,
			Active:       hh.Active,
			CursorAPIKey: hh.CursorAPIKey,
			Service:      hh.Service,
			Error:        hh.Error,
		}
		runners = append(runners, rs)
		if hh.Role == "primary" {
			primary = rs
		}
		if hh.Active {
			active = rs
		}
	}
	// remediation_runner (back-compat) reflects the active endpoint, falling
	// back to primary or the first entry.
	runner := active
	if runner.URL == "" {
		runner = primary
	}
	if runner.URL == "" && len(runners) > 0 {
		runner = runners[0]
	}

	// nightly drift only runs on the primary host
	runnerURL := h.runner.PrimaryURL()
	if runnerURL == "" {
		runnerURL = strings.TrimRight(os.Getenv("REMEDIATION_RUNNER_URL"), "/")
	}
	if runnerURL == "" {
		runnerURL = "http://127.0.0.1:8781"
	}

	hermes := probeHermesMcp(ctx, h.httpClient)
	nousHermes := probeNousHermes(ctx, h.httpClient)
	gitBridge := probeGitBridge(ctx, h.httpClient)

	tools := mcp.Catalog()
	agentTools := 0
	impl := 0
	for _, t := range tools {
		if t.Implemented {
			impl++
		}
		if t.Phase == "Agent" {
			agentTools++
		}
	}

	scriptPath := resolveMcpScriptPath()

	nightly := probeNightlyReport(ctx, h.httpClient, runnerURL)

	writeJSON(w, http.StatusOK, BridgeResponse{
		GeneratedAt:       now,
		RemediationRunner: runner,
		Runners:           runners,
		GitBridge:         gitBridge,
		HermesMcp:         hermes,
		NousHermes:        nousHermes,
		PlatformMcp: PlatformMcpStatus{
			ServerName:       mcp.ServerName,
			ServerVersion:    mcp.ServerVersion,
			ToolCount:        len(tools),
			ImplementedCount: impl,
			AgentToolCount:   agentTools,
			Transport:        "stdio",
			ScriptPath:       scriptPath,
		},
		NightlyReport: nightly,
	})
}

func (h *Handler) HandleSmoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	url := h.runner.ActiveURL()
	if url == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no runner configured"})
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/smoke", nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func probeGitBridge(ctx context.Context, client *http.Client) GitBridgeStatus {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("GIT_BRIDGE_URL")), "/")
	if url == "" {
		return GitBridgeStatus{
			Status: "not_configured",
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/status", nil)
	if err != nil {
		return GitBridgeStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return GitBridgeStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return GitBridgeStatus{URL: url, Status: "unavailable", Error: "HTTP " + resp.Status}
	}
	var body struct {
		Workspace  string   `json:"workspace"`
		DirtyRepos []string `json:"dirty_repos"`
		Repos      []struct {
			Repo string `json:"repo"`
		} `json:"repos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return GitBridgeStatus{URL: url, Status: "ok"}
	}
	return GitBridgeStatus{
		URL:        url,
		Status:     "ok",
		Workspace:  body.Workspace,
		RepoCount:  len(body.Repos),
		DirtyRepos: len(body.DirtyRepos),
	}
}

func probeHermesMcp(ctx context.Context, client *http.Client) OptionalEndpoint {
	// Try HERMES_GATEWAY_URL first (the actual gateway), fall back to HERMES_MCP_URL
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("HERMES_GATEWAY_URL")), "/")
	if url == "" {
		url = strings.TrimSpace(os.Getenv("HERMES_MCP_URL"))
	}
	if url == "" {
		return OptionalEndpoint{
			Status: "not_configured",
			Note:   "Set HERMES_GATEWAY_URL to enable Hermes Gateway health probing",
		}
	}
	healthURL := url + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return OptionalEndpoint{URL: url, Status: "unavailable", Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return OptionalEndpoint{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return OptionalEndpoint{
			URL:    url,
			Status: "unavailable",
			Error:  "HTTP " + resp.Status,
		}
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
		if v, ok := body["version"].(string); ok {
			return OptionalEndpoint{URL: url, Status: "ok", Note: "v" + v}
		}
	}
	return OptionalEndpoint{URL: url, Status: "ok"}
}

func probeNousHermes(ctx context.Context, client *http.Client) NousHermesStatus {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("NOUS_HERMES_URL")), "/")
	if url == "" {
		return NousHermesStatus{Status: "not_configured"}
	}
	apiURL := url + "/api/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return NousHermesStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	user := strings.TrimSpace(os.Getenv("NOUS_HERMES_USER"))
	pass := strings.TrimSpace(os.Getenv("NOUS_HERMES_PASS"))
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return NousHermesStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return NousHermesStatus{URL: url, Status: "auth_required", Error: "dashboard requires authentication"}
	}
	if resp.StatusCode >= 400 {
		return NousHermesStatus{URL: url, Status: "unavailable", Error: "HTTP " + resp.Status}
	}
	var body struct {
		Version        string `json:"version"`
		ReleaseDate    string `json:"release_date"`
		GatewayRunning bool   `json:"gateway_running"`
		GatewayState   string `json:"gateway_state"`
		ActiveAgents   int    `json:"active_agents"`
		ActiveSessions int    `json:"active_sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return NousHermesStatus{URL: url, Status: "ok"}
	}
	return NousHermesStatus{
		URL:            url,
		Status:         "ok",
		Version:        body.Version,
		ReleaseDate:    body.ReleaseDate,
		GatewayRunning: body.GatewayRunning,
		GatewayState:   body.GatewayState,
		ActiveAgents:   body.ActiveAgents,
		ActiveSessions: body.ActiveSessions,
		DashboardURL:   url,
	}
}

func probeNightlyReport(ctx context.Context, client *http.Client, runnerURL string) NightlyHint {
	if runnerURL == "" {
		return NightlyHint{
			Available: false,
			Hint:      "REMEDIATION_RUNNER_URL not set",
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runnerURL+"/reports/latest", nil)
	if err != nil {
		return NightlyHint{Available: false, Hint: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return NightlyHint{Available: false, Hint: "Runner report unreachable"}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return NightlyHint{Available: false, Hint: "No nightly report on runner yet"}
	}
	var out struct {
		Source    string `json:"source"`
		UpdatedAt string `json:"updated_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return NightlyHint{Available: false, Hint: "Invalid report payload"}
	}
	return NightlyHint{
		Available:   true,
		GeneratedAt: out.UpdatedAt,
		Source:      out.Source,
	}
}

func resolveMcpScriptPath() string {
	const rel = "mcp/platform/src/index.ts"
	if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
		p := root + "/" + rel
		return p
	}
	return rel
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
