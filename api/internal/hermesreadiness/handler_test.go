package hermesreadiness

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFirstTask_L0ReadOnly(t *testing.T) {
	task := FirstTask()
	if task.Autonomy != "L0" {
		t.Fatalf("autonomy %s", task.Autonomy)
	}
	if task.ID != "hermes-mission-health-l0" {
		t.Fatalf("id %s", task.ID)
	}
	if !strings.Contains(task.Prompt, "verify_mission_snapshot") {
		t.Fatal("prompt missing verify_mission_snapshot")
	}
	if !strings.Contains(task.Prompt, "first-class MCP") {
		t.Fatal("prompt must require first-class MCP verify tools")
	}
	if !strings.Contains(task.Prompt, "L0") {
		t.Fatal("prompt missing L0")
	}
	if len(task.RequiredMcpTools) < 3 {
		t.Fatalf("tools %v", task.RequiredMcpTools)
	}
}

func TestBuild_LlmKeyMissingBlockerDetail(t *testing.T) {
	t.Setenv("NOUS_HERMES_URL", "")
	t.Setenv("HERMES_LLM_KEY_CONFIGURED", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	resp := Build(t.Context(), NewHandler().httpClient)
	if resp.Ready {
		t.Fatal("expected not ready without nous + llm")
	}
	if len(resp.BlockerDetails) == 0 {
		t.Fatal("expected blocker_details")
	}
	found := false
	for _, d := range resp.BlockerDetails {
		if d.Code == "NOUS_HERMES_URL_MISSING" {
			found = true
			if d.Remediation == "" {
				t.Fatal("missing remediation")
			}
		}
	}
	if !found {
		t.Fatalf("codes %v", resp.BlockerDetails)
	}
}

func TestBuild_DeepseekKeyFromDashboard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version":         "0.17.0",
				"gateway_running": true,
				"gateway_state":   "running",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/auth/password-login":
			var body struct {
				Provider string `json:"provider"`
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad json", 400)
				return
			}
			if body.Provider != "basic" || body.Username != "bifrost" || body.Password != "secret" {
				http.Error(w, "invalid", 401)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "hermes_session_at", Value: "tok", Path: "/"})
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "next": "/"})
		case r.URL.Path == "/api/env":
			if _, err := r.Cookie("hermes_session_at"); err != nil {
				http.Error(w, `{"error":"unauthenticated"}`, 401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"DEEPSEEK_API_KEY": map[string]any{"is_set": true},
				"OPENAI_API_KEY":   map[string]any{"is_set": false},
			})
		case r.URL.Path == "/api/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"model": "deepseek/deepseek-chat", "mcp_servers": map[string]any{}})
		case r.URL.Path == "/api/mcp/servers":
			enabled := true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"servers": []map[string]any{{"name": "bifrost-platform", "enabled": enabled}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("NOUS_HERMES_URL", srv.URL)
	t.Setenv("NOUS_HERMES_USER", "bifrost")
	t.Setenv("NOUS_HERMES_PASS", "secret")
	t.Setenv("HERMES_LLM_KEY_CONFIGURED", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	resp := NewHandler().Build(t.Context())
	if !resp.Ready {
		t.Fatalf("expected ready, blockers=%v llm=%+v", resp.Blockers, resp.LlmKey)
	}
	if !resp.LlmKey.Configured || resp.LlmKey.ProviderHint != "deepseek" {
		t.Fatalf("llm %+v", resp.LlmKey)
	}
	if resp.LlmKey.Source != "nous_hermes_dashboard" {
		t.Fatalf("source %s", resp.LlmKey.Source)
	}
	if !resp.NousHermes.LlmKeyConfigured {
		t.Fatal("nous llm_key_configured")
	}
	if resp.NousHermes.McpToolCount != 1 {
		t.Fatalf("mcp count %d", resp.NousHermes.McpToolCount)
	}
	if !strings.Contains(resp.LlmKey.Note, "deepseek/deepseek-chat") {
		t.Fatalf("note %s", resp.LlmKey.Note)
	}
}

func TestFirstSetLLMKey_Deepseek(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"is_set": true})
	env := map[string]json.RawMessage{"DEEPSEEK_API_KEY": raw}
	hint, ok := firstSetLLMKey(env)
	if !ok || hint != "deepseek" {
		t.Fatalf("hint=%s ok=%v", hint, ok)
	}
}
