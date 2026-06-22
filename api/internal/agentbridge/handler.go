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
	HermesMcp         OptionalEndpoint  `json:"hermes_mcp"`
	PlatformMcp       PlatformMcpStatus `json:"platform_mcp"`
	NightlyReport     NightlyHint       `json:"nightly_report"`
}

type RunnerStatus struct {
	URL           string `json:"url"`
	Status        string `json:"status"`
	CursorAPIKey  bool   `json:"cursor_api_key,omitempty"`
	Service       string `json:"service,omitempty"`
	Error         string `json:"error,omitempty"`
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

	runnerURL := strings.TrimRight(os.Getenv("REMEDIATION_RUNNER_URL"), "/")
	if runnerURL == "" {
		runnerURL = "http://127.0.0.1:8781"
	}

	runner := RunnerStatus{URL: runnerURL, Status: "unavailable"}
	health, err := h.runner.Health(ctx)
	if err != nil {
		runner.Error = err.Error()
	} else {
		runner.Status = "ok"
		if s, ok := health["status"].(string); ok && s != "" {
			runner.Status = s
		}
		if svc, ok := health["service"].(string); ok {
			runner.Service = svc
		}
		if key, ok := health["cursor_api_key"].(bool); ok {
			runner.CursorAPIKey = key
		}
	}

	hermes := probeHermesMcp(ctx, h.httpClient)

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
		GeneratedAt: now,
		RemediationRunner: runner,
		HermesMcp:         hermes,
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

func probeHermesMcp(ctx context.Context, client *http.Client) OptionalEndpoint {
	url := strings.TrimSpace(os.Getenv("HERMES_MCP_URL"))
	if url == "" {
		return OptionalEndpoint{
			Status: "not_configured",
			Note:   "Optional — set HERMES_MCP_URL when Hermes Gateway MCP is deployed on agent host",
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return OptionalEndpoint{URL: url, Status: "unavailable", Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return OptionalEndpoint{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 500 {
		return OptionalEndpoint{
			URL:    url,
			Status: "unavailable",
			Error:  "HTTP " + resp.Status,
		}
	}
	return OptionalEndpoint{URL: url, Status: "ok"}
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
