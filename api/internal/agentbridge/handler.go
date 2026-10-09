package agentbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
)

// Handler aggregates autonomous agent host + MCP bridge status for Console.
type Handler struct {
	httpClient *http.Client
}

func NewHandler() *Handler {
	return &Handler{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type BridgeResponse struct {
	GeneratedAt          time.Time                  `json:"generated_at"`
	RemediationRunner    RunnerStatus               `json:"remediation_runner"`
	Runners              []RunnerStatus             `json:"runners"`
	GitBridge            GitBridgeStatus            `json:"git_bridge"`
	SatelliteProbeBridge SatelliteProbeBridgeStatus `json:"satellite_probe_bridge"`
	PlatformMcp          PlatformMcpStatus          `json:"platform_mcp"`
	NightlyReport        NightlyHint                `json:"nightly_report"`
}

type GitBridgeStatus struct {
	URL              string               `json:"url,omitempty"`
	Status           string               `json:"status"` // not_configured | ok | unavailable
	Workspace        string               `json:"workspace,omitempty"`
	RepoCount        int                  `json:"repo_count,omitempty"`
	DirtyRepos       int                  `json:"dirty_repos,omitempty"`
	DirtyRepoDetails []GitDirtyRepoDetail `json:"dirty_repo_details,omitempty"`
	Error            string               `json:"error,omitempty"`
}

// GitDirtyRepoDetail is a compact dirty summary for Console (repos / files / +N/−M).
type GitDirtyRepoDetail struct {
	Repo       string   `json:"repo"`
	Branch     string   `json:"branch,omitempty"`
	Staged     []string `json:"staged,omitempty"`
	Modified   []string `json:"modified,omitempty"`
	Untracked  []string `json:"untracked,omitempty"`
	Insertions int      `json:"insertions"`
	Deletions  int      `json:"deletions"`
}

type SatelliteProbeBridgeStatus struct {
	URL            string `json:"url,omitempty"`
	Status         string `json:"status"` // not_configured | ok | unavailable
	TradeNginxBase string `json:"trade_nginx_base,omitempty"`
	Error          string `json:"error,omitempty"`
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

type NightlyHint struct {
	Available   bool   `json:"available"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Source      string `json:"source,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

func (h *Handler) HandleBridge(w http.ResponseWriter, r *http.Request) {
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

	gitBridge := probeGitBridge(ctx, h.httpClient)
	satelliteProbeBridge := probeSatelliteProbeBridge(ctx, h.httpClient)

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
		GeneratedAt:          now,
		RemediationRunner:    runner,
		Runners:              runners,
		GitBridge:            gitBridge,
		SatelliteProbeBridge: satelliteProbeBridge,
		PlatformMcp: PlatformMcpStatus{
			ServerName:       mcp.ServerName,
			ServerVersion:    mcp.ServerVersion,
			ToolCount:        len(tools),
			ImplementedCount: impl,
			AgentToolCount:   agentTools,
			Transport:        "stdio",
			ScriptPath:       scriptPath,
		},
		NightlyReport: NightlyHint{
			Available: false,
			Hint:      "已退役: nightly report is not served by the operator plane",
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

func setGitBridgeAuth(req *http.Request) {
	// STG and the Mac Pro process export PLATFORM_OPERATOR_TOKEN. PROD
	// exports PLATFORM_PROD_OPERATOR_TOKEN. Git-bridge accepts a value only
	// when it equals an operator or admin token loaded on the bridge host.
	for _, key := range []string{
		"PLATFORM_OPERATOR_TOKEN",
		"PLATFORM_ADMIN_TOKEN",
		"PLATFORM_PROD_OPERATOR_TOKEN",
		"PLATFORM_PROD_ADMIN_TOKEN",
	} {
		token := strings.TrimSpace(os.Getenv(key))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
			return
		}
	}
}

// localOnlyBridgeText is returned when GIT_BRIDGE_URL or
// SATELLITE_PROBE_BRIDGE_URL is unset. Those bridges run on a dev
// workstation; an unset URL is not a failure.
const localOnlyBridgeText = "local-only (dev workstation)"

func probeGitBridge(ctx context.Context, client *http.Client) GitBridgeStatus {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("GIT_BRIDGE_URL")), "/")
	if url == "" {
		return GitBridgeStatus{
			Status: "not_configured",
			Error:  localOnlyBridgeText,
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/status", nil)
	if err != nil {
		return GitBridgeStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	setGitBridgeAuth(req)
	resp, err := client.Do(req)
	if err != nil {
		return GitBridgeStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return GitBridgeStatus{URL: url, Status: "unavailable", Error: "HTTP " + resp.Status}
	}
	var body struct {
		Workspace  string   `json:"workspace"`
		DirtyRepos []string `json:"dirty_repos"`
		Repos      []struct {
			Repo       string   `json:"repo"`
			Branch     string   `json:"branch"`
			Dirty      bool     `json:"dirty"`
			Staged     []string `json:"staged"`
			Modified   []string `json:"modified"`
			Untracked  []string `json:"untracked"`
			Insertions int      `json:"insertions"`
			Deletions  int      `json:"deletions"`
		} `json:"repos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return GitBridgeStatus{URL: url, Status: "ok"}
	}
	details := make([]GitDirtyRepoDetail, 0)
	for _, r := range body.Repos {
		if !r.Dirty {
			continue
		}
		details = append(details, GitDirtyRepoDetail{
			Repo:       r.Repo,
			Branch:     r.Branch,
			Staged:     r.Staged,
			Modified:   r.Modified,
			Untracked:  r.Untracked,
			Insertions: r.Insertions,
			Deletions:  r.Deletions,
		})
	}
	dirtyCount := len(body.DirtyRepos)
	if dirtyCount == 0 {
		dirtyCount = len(details)
	}
	return GitBridgeStatus{
		URL:              url,
		Status:           "ok",
		Workspace:        body.Workspace,
		RepoCount:        len(body.Repos),
		DirtyRepos:       dirtyCount,
		DirtyRepoDetails: details,
	}
}

func probeSatelliteProbeBridge(ctx context.Context, client *http.Client) SatelliteProbeBridgeStatus {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("SATELLITE_PROBE_BRIDGE_URL")), "/")
	if url == "" {
		return SatelliteProbeBridgeStatus{
			Status: "not_configured",
			Error:  localOnlyBridgeText,
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/health", nil)
	if err != nil {
		return SatelliteProbeBridgeStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return SatelliteProbeBridgeStatus{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return SatelliteProbeBridgeStatus{URL: url, Status: "unavailable", Error: "HTTP " + resp.Status}
	}
	var body struct {
		TradeNginxBase string `json:"trade_nginx_base"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return SatelliteProbeBridgeStatus{URL: url, Status: "ok"}
	}
	return SatelliteProbeBridgeStatus{
		URL:            url,
		Status:         "ok",
		TradeNginxBase: body.TradeNginxBase,
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
