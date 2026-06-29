package remediation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type endpoint struct {
	url  string
	role string // primary | standby
}

// RunnerClient talks to one or two remediation runners (Active-Standby).
// Requests are tried against the active endpoint first; on a connection
// failure the client transparently fails over to the standby and remembers
// the new active endpoint for subsequent calls.
type RunnerClient struct {
	endpoints  []endpoint
	mu         sync.Mutex
	activeIdx  int
	httpClient *http.Client
}

func NewRunnerClient() *RunnerClient {
	var eps []endpoint
	primary := strings.TrimRight(strings.TrimSpace(os.Getenv("REMEDIATION_RUNNER_URL")), "/")
	if primary == "" {
		primary = "http://127.0.0.1:8781"
	}
	eps = append(eps, endpoint{url: primary, role: "primary"})
	standby := strings.TrimRight(strings.TrimSpace(os.Getenv("REMEDIATION_RUNNER_STANDBY_URL")), "/")
	if standby != "" && standby != primary {
		eps = append(eps, endpoint{url: standby, role: "standby"})
	}
	return &RunnerClient{
		endpoints: eps,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ordered returns endpoints with the current active one first.
func (c *RunnerClient) ordered() []endpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.endpoints) <= 1 {
		return append([]endpoint(nil), c.endpoints...)
	}
	out := make([]endpoint, 0, len(c.endpoints))
	out = append(out, c.endpoints[c.activeIdx])
	for i, e := range c.endpoints {
		if i != c.activeIdx {
			out = append(out, e)
		}
	}
	return out
}

func (c *RunnerClient) markActive(url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.endpoints {
		if e.url == url {
			c.activeIdx = i
			return
		}
	}
}

// PrimaryURL returns the configured primary endpoint URL (nightly drift target).
func (c *RunnerClient) PrimaryURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.endpoints {
		if e.role == "primary" {
			return e.url
		}
	}
	if len(c.endpoints) > 0 {
		return c.endpoints[0].url
	}
	return ""
}

// ActiveURL returns the endpoint currently selected for routing.
func (c *RunnerClient) ActiveURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.endpoints) == 0 {
		return ""
	}
	return c.endpoints[c.activeIdx].url
}

// do issues a request against the active endpoint, failing over to the
// standby on connection errors and remembering the working endpoint.
func (c *RunnerClient) do(ctx context.Context, method, suffix string, body []byte) (*http.Response, error) {
	var lastErr error
	for _, ep := range c.ordered() {
		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, ep.url+suffix, reader)
		if err != nil {
			lastErr = err
			continue
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		c.markActive(ep.url)
		return resp, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no runner endpoints configured")
	}
	return nil, lastErr
}

// RunnerHealth is a per-endpoint health snapshot for dual-heartbeat display.
type RunnerHealth struct {
	URL          string `json:"url"`
	Role         string `json:"role"`
	Status       string `json:"status"` // ok | unavailable
	Version      string `json:"version,omitempty"`
	Service      string `json:"service,omitempty"`
	CursorAPIKey bool   `json:"cursor_api_key,omitempty"`
	Active       bool   `json:"active"`
	Error        string `json:"error,omitempty"`
}

// HealthAll probes every endpoint independently (for Console dual heartbeat),
// then re-selects the active endpoint: keep the current one if healthy,
// otherwise fail over to the first healthy endpoint.
func (c *RunnerClient) HealthAll(ctx context.Context) []RunnerHealth {
	c.mu.Lock()
	eps := append([]endpoint(nil), c.endpoints...)
	activeURL := ""
	if len(c.endpoints) > 0 {
		activeURL = c.endpoints[c.activeIdx].url
	}
	c.mu.Unlock()

	out := make([]RunnerHealth, 0, len(eps))
	for _, ep := range eps {
		h := RunnerHealth{URL: ep.url, Role: ep.role, Status: "unavailable", Active: ep.url == activeURL}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.url+"/health", nil)
		if err != nil {
			h.Error = err.Error()
			out = append(out, h)
			continue
		}
		resp, err := c.httpClient.Do(req)
		if err != nil {
			h.Error = err.Error()
			out = append(out, h)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode >= 300 {
				h.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				h.Status = "ok"
				return
			}
			h.Status = "ok"
			if s, ok := body["status"].(string); ok && s != "" {
				h.Status = s
			}
			if v, ok := body["version"].(string); ok {
				h.Version = v
			}
			if svc, ok := body["service"].(string); ok {
				h.Service = svc
			}
			if k, ok := body["cursor_api_key"].(bool); ok {
				h.CursorAPIKey = k
			}
		}()
		out = append(out, h)
	}

	c.updateActiveFromHealth(out)
	// recompute Active flag after possible failover
	newActive := c.ActiveURL()
	for i := range out {
		out[i].Active = out[i].URL == newActive
	}
	return out
}

func (c *RunnerClient) updateActiveFromHealth(hs []RunnerHealth) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.endpoints) == 0 {
		return
	}
	// Prefer endpoints in configured order (primary first): select the first
	// healthy one. This keeps the primary active whenever it is healthy and
	// only falls back to the standby while the primary is down — fail-back is
	// automatic once the primary recovers.
	for i, e := range c.endpoints {
		for _, h := range hs {
			if h.URL == e.url && h.Status == "ok" {
				c.activeIdx = i
				return
			}
		}
	}
}

func (c *RunnerClient) Health(ctx context.Context) (map[string]any, error) {
	resp, err := c.do(ctx, http.MethodGet, "/health", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("runner health: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *RunnerClient) Start(ctx context.Context, body StartRunnerRequest) (*Job, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/run", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("runner start: HTTP %d %s", resp.StatusCode, trimRunnerErrorBody(raw, resp.StatusCode))
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *RunnerClient) Get(ctx context.Context, id string) (*Job, error) {
	resp, err := c.do(ctx, http.MethodGet, "/run/"+id, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("runner get: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *RunnerClient) Cancel(ctx context.Context, id string) (*Job, error) {
	resp, err := c.do(ctx, http.MethodPost, "/run/"+id+"/cancel", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("runner cancel: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *RunnerClient) Stream(ctx context.Context, id string, onLine func([]byte) error) error {
	var lastErr error
	for _, ep := range c.ordered() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.url+"/run/"+id+"/stream", nil)
		if err != nil {
			lastErr = err
			continue
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		c.markActive(ep.url)
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("runner stream: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 || line[0] == ':' {
				continue
			}
			if !bytes.HasPrefix(line, []byte("data: ")) {
				continue
			}
			payload := bytes.TrimPrefix(line, []byte("data: "))
			if err := onLine(payload); err != nil {
				return err
			}
		}
		return scanner.Err()
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no runner endpoints configured")
	}
	return lastErr
}

func (c *RunnerClient) Respond(ctx context.Context, id string, body RespondRequest) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, http.MethodPost, "/run/"+id+"/respond", payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("runner respond: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (c *RunnerClient) List(ctx context.Context) ([]Job, error) {
	resp, err := c.do(ctx, http.MethodGet, "/run", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("runner list: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

type NightlyRunResponse struct {
	Status     string `json:"status"`
	Script     string `json:"script,omitempty"`
	LogPath    string `json:"log_path,omitempty"`
	ReportsDir string `json:"reports_dir,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (c *RunnerClient) TriggerNightly(ctx context.Context) (*NightlyRunResponse, error) {
	resp, err := c.do(ctx, http.MethodPost, "/nightly/run", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("runner nightly: HTTP %d %s", resp.StatusCode, trimRunnerErrorBody(raw, resp.StatusCode))
	}
	var out NightlyRunResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func trimRunnerErrorBody(raw []byte, status int) string {
	s := strings.TrimSpace(string(raw))
	if strings.HasPrefix(s, "<") {
		if idx := strings.Index(s, "ReferenceError:"); idx >= 0 {
			chunk := s[idx:]
			if end := strings.Index(chunk, "<"); end > 0 {
				chunk = chunk[:end]
			}
			return strings.TrimSpace(chunk)
		}
		if idx := strings.Index(s, "Error:"); idx >= 0 {
			chunk := s[idx:]
			if end := strings.Index(chunk, "<"); end > 0 {
				chunk = chunk[:end]
			}
			return strings.TrimSpace(chunk)
		}
		return fmt.Sprintf("runner error (HTTP %d)", status)
	}
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}
