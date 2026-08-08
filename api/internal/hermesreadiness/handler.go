package hermesreadiness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
)

type Handler struct {
	httpClient  *http.Client
	dashMu      sync.Mutex
	dashCache   *dashProbeResult
	dashCacheAt time.Time
}

func NewHandler() *Handler {
	jar, _ := cookiejar.New(nil)
	return &Handler{httpClient: &http.Client{Timeout: 15 * time.Second, Jar: jar}}
}

func (h *Handler) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.Build(r.Context()))
}

func (h *Handler) Build(ctx context.Context) ReadinessResponse {
	return buildReadiness(ctx, h)
}

// Build is the test/package entry used by existing unit tests.
func Build(ctx context.Context, client *http.Client) ReadinessResponse {
	return (&Handler{httpClient: ensureJar(client)}).Build(ctx)
}

func (h *Handler) HandleFirstTask(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"first_task":   FirstTask(),
	})
}

func buildReadiness(ctx context.Context, h *Handler) ReadinessResponse {
	now := time.Now().UTC()
	client := ensureJar(h.httpClient)
	nous := probeNousHermes(ctx, client)
	var dash dashProbeResult
	if nous.Status == "ok" && nous.URL != "" {
		dash = h.cachedDashProbe(ctx, nous.URL)
		if dash.mcpCount > 0 {
			nous.McpToolCount = dash.mcpCount
		}
		if dash.llm.Configured {
			nous.LlmKeyConfigured = true
		}
	}
	llm := probeLlmKey(nous)
	if dash.llm.Configured {
		llm = dash.llm
	} else if !llm.Configured && dash.llm.Source != "" {
		llm = dash.llm
	}
	tools := mcp.Catalog()
	agentTools := 0
	for _, t := range tools {
		if t.Phase == "Agent" && t.Implemented {
			agentTools++
		}
	}

	blockers := make([]string, 0, 4)
	details := make([]BlockerDetail, 0, 4)
	ready := true

	addBlocker := func(code, message, remediation string, ownerAction bool) {
		blockers = append(blockers, message)
		details = append(details, BlockerDetail{
			Code:        code,
			Message:     message,
			Remediation: remediation,
			OwnerAction: ownerAction,
		})
	}

	if nous.Status != "ok" {
		ready = false
		if nous.Status == "not_configured" {
			addBlocker(
				"NOUS_HERMES_URL_MISSING",
				"NOUS_HERMES_URL not configured on platform-api",
				"Set NOUS_HERMES_URL (and NOUS_HERMES_USER/PASS if required) on platform-api, then re-probe GET /api/v1/agent/hermes/readiness",
				false,
			)
		} else {
			msg := "Nous Hermes Agent unreachable: " + nous.Status
			if nous.Error != "" {
				msg += " (" + nous.Error + ")"
			}
			addBlocker(
				"NOUS_HERMES_UNREACHABLE",
				msg,
				"Confirm Nous Hermes dashboard on Mac Mini primary is running and reachable from platform-api; check NOUS_HERMES_URL and auth",
				false,
			)
		}
	} else if !nous.GatewayRunning {
		ready = false
		addBlocker(
			"HERMES_GATEWAY_DOWN",
			"Hermes gateway not running",
			"Start Nous Hermes gateway on the agent host (Mac Mini primary); confirm gateway_running=true in /api/status",
			false,
		)
	}

	if !llm.Configured {
		ready = false
		hostHint := strings.TrimSpace(nous.URL)
		if hostHint == "" {
			hostHint = "Mac Mini primary (Nous Hermes host)"
		}
		addBlocker(
			"LLM_KEY_MISSING",
			"LLM API key not configured on Nous Hermes host ("+hostHint+")",
			"Owner: add provider key to ~/.hermes/.env on the Nous Hermes host (not platform-api). Do not commit keys. Re-probe GET /api/v1/agent/hermes/readiness after config.",
			true,
		)
	}

	if agentTools < 4 {
		ready = false
		addBlocker(
			"PLATFORM_MCP_INCOMPLETE",
			"platform MCP agent tools incomplete",
			"Ensure platform MCP catalog exposes Agent-phase tools (get_hermes_readiness, verify_mission_snapshot, get_connectivity_matrix, verify_payload); rebuild platform-api if catalog changed",
			false,
		)
	}

	return ReadinessResponse{
		GeneratedAt:      now,
		Ready:            ready,
		Blockers:         blockers,
		BlockerDetails:   details,
		LlmKey:           llm,
		NousHermes:       nous,
		PlatformMcpTools: len(tools),
		PlatformMcpAgent: agentTools,
		FirstTask:        FirstTask(),
	}
}

func probeLlmKey(nous NousHermesProbe) LlmKeyStatus {
	if nous.LlmKeyConfigured {
		return LlmKeyStatus{
			Configured:   true,
			Source:       "nous_hermes_api",
			ProviderHint: "configured_on_agent_host",
			Note:         "Reported by Nous Hermes /api/status",
		}
	}
	if v := strings.TrimSpace(os.Getenv("HERMES_LLM_KEY_CONFIGURED")); v == "1" || strings.EqualFold(v, "true") {
		return LlmKeyStatus{
			Configured: true,
			Source:     "env_override",
			Note:       "HERMES_LLM_KEY_CONFIGURED set on platform-api (dev hint)",
		}
	}
	for _, pair := range []struct {
		env, provider string
	}{
		{"ANTHROPIC_API_KEY", "anthropic"},
		{"OPENROUTER_API_KEY", "openrouter"},
		{"OPENAI_API_KEY", "openai"},
	} {
		if strings.TrimSpace(os.Getenv(pair.env)) != "" {
			return LlmKeyStatus{
				Configured:   true,
				Source:       "platform_api_env",
				ProviderHint: pair.provider,
				Note:         "Key present on platform-api host (local dev); production keys live on Mac Mini agent host",
			}
		}
	}
	return LlmKeyStatus{
		Configured: false,
		Source:     "unknown",
		Note:       "Owner configures LLM provider in ~/.hermes/.env on Nous Hermes host (Mac Mini primary); Agent must not set keys. Re-probe GET /api/v1/agent/hermes/readiness",
	}
}

func probeNousHermes(ctx context.Context, client *http.Client) NousHermesProbe {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("NOUS_HERMES_URL")), "/")
	if url == "" {
		return NousHermesProbe{Status: "not_configured"}
	}
	apiURL := url + "/api/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return NousHermesProbe{URL: url, Status: "unavailable", Error: err.Error()}
	}
	user := strings.TrimSpace(os.Getenv("NOUS_HERMES_USER"))
	pass := strings.TrimSpace(os.Getenv("NOUS_HERMES_PASS"))
	if user != "" && pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := client.Do(req)
	if err != nil {
		return NousHermesProbe{URL: url, Status: "unavailable", Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return NousHermesProbe{URL: url, Status: "auth_required", Error: "dashboard requires authentication"}
	}
	if resp.StatusCode >= 400 {
		return NousHermesProbe{URL: url, Status: "unavailable", Error: "HTTP " + resp.Status}
	}
	var body struct {
		Version          string `json:"version"`
		GatewayRunning   bool   `json:"gateway_running"`
		GatewayState     string `json:"gateway_state"`
		McpToolCount     int    `json:"mcp_tool_count"`
		LlmKeyConfigured bool   `json:"llm_key_configured"`
		LlmConfigured    bool   `json:"llm_configured"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return NousHermesProbe{URL: url, Status: "ok", DashboardURL: url}
	}
	llmOK := body.LlmKeyConfigured || body.LlmConfigured
	return NousHermesProbe{
		URL:              url,
		Status:           "ok",
		Version:          body.Version,
		GatewayRunning:   body.GatewayRunning,
		GatewayState:     body.GatewayState,
		McpToolCount:     body.McpToolCount,
		LlmKeyConfigured: llmOK,
		DashboardURL:     url,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
