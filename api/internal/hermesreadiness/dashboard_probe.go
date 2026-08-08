package hermesreadiness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strings"
	"time"
)

const dashCacheTTL = 45 * time.Second

// Known Hermes /api/env LLM key names → provider hint. Values are never read.
var llmEnvProviders = []struct {
	env, hint string
}{
	{"DEEPSEEK_API_KEY", "deepseek"},
	{"ANTHROPIC_API_KEY", "anthropic"},
	{"ANTHROPIC_TOKEN", "anthropic"},
	{"OPENROUTER_API_KEY", "openrouter"},
	{"OPENAI_API_KEY", "openai"},
	{"GLM_API_KEY", "zai"},
	{"ZAI_API_KEY", "zai"},
	{"Z_AI_API_KEY", "zai"},
	{"KIMI_API_KEY", "kimi"},
	{"KIMI_CODING_API_KEY", "kimi"},
	{"MINIMAX_API_KEY", "minimax"},
	{"GOOGLE_API_KEY", "google"},
	{"GEMINI_API_KEY", "google"},
	{"XAI_API_KEY", "xai"},
	{"DASHSCOPE_API_KEY", "qwen"},
	{"MISTRAL_API_KEY", "mistral"},
}

type dashProbeResult struct {
	llm      LlmKeyStatus
	mcpCount int
	model    string
}

func ensureJar(client *http.Client) *http.Client {
	if client == nil {
		jar, _ := cookiejar.New(nil)
		return &http.Client{Timeout: 15 * time.Second, Jar: jar}
	}
	if client.Jar != nil {
		return client
	}
	jar, _ := cookiejar.New(nil)
	clone := *client
	clone.Jar = jar
	if clone.Timeout == 0 {
		clone.Timeout = 15 * time.Second
	}
	return &clone
}

func (h *Handler) cachedDashProbe(ctx context.Context, baseURL string) dashProbeResult {
	h.dashMu.Lock()
	defer h.dashMu.Unlock()
	if h.dashCache != nil && time.Since(h.dashCacheAt) < dashCacheTTL {
		return *h.dashCache
	}
	res := probeHermesDashboard(ctx, h.httpClient, baseURL)
	h.dashCache = &res
	h.dashCacheAt = time.Now()
	return res
}

func probeHermesDashboard(ctx context.Context, client *http.Client, baseURL string) dashProbeResult {
	client = ensureJar(client)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return dashProbeResult{}
	}

	user := strings.TrimSpace(os.Getenv("NOUS_HERMES_USER"))
	pass := strings.TrimSpace(os.Getenv("NOUS_HERMES_PASS"))
	if user == "" || pass == "" {
		return dashProbeResult{}
	}

	if err := hermesPasswordLogin(ctx, client, baseURL, user, pass); err != nil {
		return dashProbeResult{
			llm: LlmKeyStatus{
				Configured: false,
				Source:     "nous_hermes_dashboard",
				Note:       "dashboard login failed: " + err.Error(),
			},
		}
	}

	envBody, err := hermesGETJSON(ctx, client, baseURL+"/api/env")
	if err != nil {
		return dashProbeResult{
			llm: LlmKeyStatus{
				Configured: false,
				Source:     "nous_hermes_dashboard",
				Note:       "GET /api/env failed: " + err.Error(),
			},
		}
	}

	hint, ok := firstSetLLMKey(envBody)
	model := fetchHermesModel(ctx, client, baseURL)
	mcpCount := fetchHermesMCPServerCount(ctx, client, baseURL)

	if !ok {
		note := "Nous Hermes /api/env reports no LLM provider key is_set"
		if model != "" {
			note += "; model=" + model
		}
		return dashProbeResult{
			llm: LlmKeyStatus{
				Configured: false,
				Source:     "nous_hermes_dashboard",
				Note:       note,
			},
			mcpCount: mcpCount,
			model:    model,
		}
	}

	note := "Nous Hermes /api/env reports " + hint + " key is_set (value never read)"
	if model != "" {
		note += "; model=" + model
	}
	return dashProbeResult{
		llm: LlmKeyStatus{
			Configured:   true,
			Source:       "nous_hermes_dashboard",
			ProviderHint: hint,
			Note:         note,
		},
		mcpCount: mcpCount,
		model:    model,
	}
}

func hermesPasswordLogin(ctx context.Context, client *http.Client, baseURL, user, pass string) error {
	payload, err := json.Marshal(map[string]string{
		"provider": "basic",
		"username": user,
		"password": pass,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/auth/password-login", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("invalid login JSON")
	}
	if !out.OK {
		return fmt.Errorf("login not ok")
	}
	return nil
}

func hermesGETJSON(ctx context.Context, client *http.Client, url string) (map[string]json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func firstSetLLMKey(env map[string]json.RawMessage) (hint string, ok bool) {
	for _, p := range llmEnvProviders {
		raw, exists := env[p.env]
		if !exists {
			continue
		}
		var entry struct {
			IsSet bool `json:"is_set"`
		}
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		if entry.IsSet {
			return p.hint, true
		}
	}
	return "", false
}

func fetchHermesModel(ctx context.Context, client *http.Client, baseURL string) string {
	raw, err := hermesGETJSON(ctx, client, baseURL+"/api/config")
	if err != nil {
		return ""
	}
	b, ok := raw["model"]
	if !ok {
		return ""
	}
	var model string
	if err := json.Unmarshal(b, &model); err != nil {
		return ""
	}
	return strings.TrimSpace(model)
}

func fetchHermesMCPServerCount(ctx context.Context, client *http.Client, baseURL string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/mcp/servers", nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return 0
	}
	var body struct {
		Servers []struct {
			Enabled *bool `json:"enabled"`
		} `json:"servers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return 0
	}
	n := 0
	for _, s := range body.Servers {
		if s.Enabled == nil || *s.Enabled {
			n++
		}
	}
	return n
}
