package hermesreadiness

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/mcp"
)

type Handler struct {
	httpClient *http.Client
}

func NewHandler() *Handler {
	return &Handler{httpClient: &http.Client{Timeout: 8 * time.Second}}
}

func (h *Handler) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Build(r.Context(), h.httpClient))
}

func (h *Handler) HandleFirstTask(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"first_task":   FirstTask(),
	})
}

func Build(ctx context.Context, client *http.Client) ReadinessResponse {
	now := time.Now().UTC()
	nous := probeNousHermes(ctx, client)
	llm := probeLlmKey(nous)
	tools := mcp.Catalog()
	agentTools := 0
	for _, t := range tools {
		if t.Phase == "Agent" && t.Implemented {
			agentTools++
		}
	}

	blockers := make([]string, 0, 4)
	ready := true

	if nous.Status != "ok" {
		ready = false
		if nous.Status == "not_configured" {
			blockers = append(blockers, "NOUS_HERMES_URL not configured on platform-api")
		} else {
			blockers = append(blockers, "Nous Hermes Agent unreachable: "+nous.Status)
		}
	} else if !nous.GatewayRunning {
		ready = false
		blockers = append(blockers, "Hermes gateway not running")
	}

	if !llm.Configured {
		ready = false
		blockers = append(blockers, "LLM API key not detected — configure in ~/.hermes/ or set ANTHROPIC_API_KEY/OPENROUTER_API_KEY on agent host")
	}

	if agentTools < 4 {
		ready = false
		blockers = append(blockers, "platform MCP agent tools incomplete")
	}

	return ReadinessResponse{
		GeneratedAt:      now,
		Ready:            ready,
		Blockers:         blockers,
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
		Note:       "Configure LLM provider in Nous Hermes (~/.hermes/) on Mac Mini primary; re-probe via GET /api/v1/agent/hermes/readiness",
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
	defer resp.Body.Close()
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
