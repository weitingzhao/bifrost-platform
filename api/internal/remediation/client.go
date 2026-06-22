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
	"time"
)

type RunnerClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewRunnerClient() *RunnerClient {
	base := os.Getenv("REMEDIATION_RUNNER_URL")
	if base == "" {
		base = "http://127.0.0.1:8781"
	}
	return &RunnerClient{
		baseURL: strings.TrimRight(base, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *RunnerClient) Health(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/run", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/run/"+id, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/run/"+id+"/cancel", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/run/"+id+"/stream", nil)
	if err != nil {
		return err
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
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

func (c *RunnerClient) Respond(ctx context.Context, id string, body RespondRequest) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/run/"+id+"/respond", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/run", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/nightly/run", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
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
