package hermesreadiness

import "time"

// LlmKeyStatus reports whether an LLM provider key is available (never exposes secrets).
type LlmKeyStatus struct {
	Configured     bool   `json:"configured"`
	Source         string `json:"source"`
	ProviderHint   string `json:"provider_hint,omitempty"`
	Note           string `json:"note,omitempty"`
}

// FirstTaskDefinition is the canonical L0 read-only Hermes onboarding task.
type FirstTaskDefinition struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Autonomy         string   `json:"autonomy"`
	Prompt           string   `json:"prompt"`
	RequiredMcpTools []string `json:"required_mcp_tools"`
	SuccessCriteria  []string `json:"success_criteria"`
}

// ReadinessResponse is returned by GET /api/v1/agent/hermes/readiness.
type ReadinessResponse struct {
	GeneratedAt      time.Time           `json:"generated_at"`
	Ready            bool                `json:"ready"`
	Blockers         []string            `json:"blockers"`
	LlmKey           LlmKeyStatus        `json:"llm_key"`
	NousHermes       NousHermesProbe     `json:"nous_hermes"`
	PlatformMcpTools int                 `json:"platform_mcp_tools"`
	PlatformMcpAgent int                 `json:"platform_mcp_agent_tools"`
	FirstTask        FirstTaskDefinition `json:"first_task"`
}

// NousHermesProbe mirrors agent bridge nous_hermes block plus readiness fields.
type NousHermesProbe struct {
	URL              string `json:"url,omitempty"`
	Status           string `json:"status"`
	Version          string `json:"version,omitempty"`
	GatewayRunning   bool   `json:"gateway_running"`
	GatewayState     string `json:"gateway_state,omitempty"`
	McpToolCount     int    `json:"mcp_tool_count"`
	LlmKeyConfigured bool   `json:"llm_key_configured"`
	DashboardURL     string `json:"dashboard_url,omitempty"`
	Error            string `json:"error,omitempty"`
}
