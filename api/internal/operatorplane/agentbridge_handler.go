package operatorplane

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
)

// AgentBridgeHandler aggregates autonomous agent host + MCP bridge status for Console.
type AgentBridgeHandler struct {
	httpClient *http.Client
}

func NewAgentBridgeHandler() *AgentBridgeHandler {
	return &AgentBridgeHandler{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type BridgeResponse struct {
	GeneratedAt       time.Time         `json:"generated_at"`
	RemediationRunner RunnerStatus      `json:"remediation_runner"`
	Runners           []RunnerStatus    `json:"runners"`
	PlatformMcp       PlatformMcpStatus `json:"platform_mcp"`
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

type PlatformMcpStatus struct {
	ServerName       string `json:"server_name"`
	ServerVersion    string `json:"server_version"`
	ToolCount        int    `json:"tool_count"`
	ImplementedCount int    `json:"implemented_count"`
	AgentToolCount   int    `json:"agent_tool_count"`
	Transport        string `json:"transport"`
	ScriptPath       string `json:"script_path"`
}

func (h *AgentBridgeHandler) HandleBridge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()

	healths := probePlanes(ctx, h.httpClient)
	runners := make([]RunnerStatus, 0, len(healths))
	var primary RunnerStatus
	for _, hh := range healths {
		runners = append(runners, hh)
		if hh.Role == "primary" {
			primary = hh
		}
	}
	runner := primary
	if runner.URL == "" && len(runners) > 0 {
		runner = runners[0]
	}

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

	writeJSON(w, http.StatusOK, BridgeResponse{
		GeneratedAt:       now,
		RemediationRunner: runner,
		Runners:           runners,
		PlatformMcp: PlatformMcpStatus{
			ServerName:       mcp.ServerName,
			ServerVersion:    mcp.ServerVersion,
			ToolCount:        len(tools),
			ImplementedCount: impl,
			AgentToolCount:   agentTools,
			Transport:        "stdio",
			ScriptPath:       scriptPath,
		},
	})
}

// probePlanes reads PLANE_HEALTH_URLS (comma-separated operator-plane bases).
// The first URL is primary, the rest are standby. Each GET {url}/health
// should return {"status":"ok","version":"<git sha>"}.
func probePlanes(ctx context.Context, client *http.Client) []RunnerStatus {
	raw := strings.TrimSpace(os.Getenv("PLANE_HEALTH_URLS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]RunnerStatus, 0, len(parts))
	for i, part := range parts {
		u := strings.TrimRight(strings.TrimSpace(part), "/")
		if u == "" {
			continue
		}
		role := "standby"
		if i == 0 {
			role = "primary"
		}
		seat := RunnerStatus{URL: u, Role: role, Service: "operator-plane"}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u+"/health", nil)
		if err != nil {
			seat.Status = "unavailable"
			seat.Error = err.Error()
			out = append(out, seat)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			seat.Status = "unavailable"
			seat.Error = err.Error()
			out = append(out, seat)
			continue
		}
		var body struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}
		decErr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 || decErr != nil || body.Status == "" {
			seat.Status = "unavailable"
			if decErr != nil {
				seat.Error = decErr.Error()
			} else {
				seat.Error = resp.Status
			}
			out = append(out, seat)
			continue
		}
		seat.Status = body.Status
		seat.Version = body.Version
		out = append(out, seat)
	}
	return out
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
