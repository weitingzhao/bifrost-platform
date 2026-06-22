package agentreport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Handler serves read-only autonomous agent nightly reports.
type Handler struct {
	runnerURL   string
	localPaths  []string
	httpClient  *http.Client
}

func NewHandler() *Handler {
	runner := strings.TrimRight(os.Getenv("REMEDIATION_RUNNER_URL"), "/")
	paths := []string{}
	if p := strings.TrimSpace(os.Getenv("AGENT_NIGHTLY_REPORT_PATH")); p != "" {
		paths = append(paths, p)
	}
	// Mac Pro dev: make nightly-agent → agent/reports/latest.md
	if root := os.Getenv("BIFROST_PLATFORM_ROOT"); root != "" {
		paths = append(paths, filepath.Join(root, "agent", "reports", "latest.md"))
	}
	paths = append(paths,
		"agent/reports/latest.md",
		filepath.Join(os.Getenv("HOME"), "bifrost-agent", "reports", "latest.md"),
	)
	return &Handler{
		runnerURL:   runner,
		localPaths:  paths,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

type NightlyReportResponse struct {
	Available   bool   `json:"available"`
	Content     string `json:"content,omitempty"`
	Source      string `json:"source,omitempty"`
	GeneratedAt string `json:"generated_at,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

func (h *Handler) HandleNightlyReport(w http.ResponseWriter, r *http.Request) {
	if h.runnerURL != "" {
		if body, src, at, ok := h.fetchFromRunner(r.Context()); ok {
			writeJSON(w, http.StatusOK, NightlyReportResponse{
				Available:   true,
				Content:     body,
				Source:      src,
				GeneratedAt: at,
			})
			return
		}
	}

	if body, path, at, ok := h.readLocal(); ok {
		writeJSON(w, http.StatusOK, NightlyReportResponse{
			Available:   true,
			Content:     body,
			Source:      path,
			GeneratedAt: at,
		})
		return
	}

	writeJSON(w, http.StatusOK, NightlyReportResponse{
		Available: false,
		Hint:      "No nightly report yet. Mac Mini: launchd 3:00 AM. Mac Pro dev: Agent Desk → Run drift scan now",
	})
}

type NightlyTriggerResponse struct {
	Status     string `json:"status"`
	Script     string `json:"script,omitempty"`
	LogPath    string `json:"log_path,omitempty"`
	ReportsDir string `json:"reports_dir,omitempty"`
	Hint       string `json:"hint,omitempty"`
	Error      string `json:"error,omitempty"`
}

func (h *Handler) HandleTriggerNightly(w http.ResponseWriter, r *http.Request) {
	if h.runnerURL == "" {
		writeJSON(w, http.StatusBadGateway, NightlyTriggerResponse{
			Status: "error",
			Error:  "REMEDIATION_RUNNER_URL not set",
		})
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.runnerURL+"/nightly/run", nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, NightlyTriggerResponse{Status: "error", Error: err.Error()})
		return
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, NightlyTriggerResponse{
			Status: "error",
			Error:  err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		writeJSON(w, resp.StatusCode, NightlyTriggerResponse{
			Status: "error",
			Error:  strings.TrimSpace(string(raw)),
		})
		return
	}
	var out NightlyTriggerResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		writeJSON(w, http.StatusInternalServerError, NightlyTriggerResponse{Status: "error", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (h *Handler) fetchFromRunner(ctx context.Context) (string, string, string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.runnerURL+"/reports/latest", nil)
	if err != nil {
		return "", "", "", false
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", "", "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", false
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", false
	}
	var out struct {
		Content    string `json:"content"`
		Source     string `json:"source"`
		UpdatedAt  string `json:"updated_at"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Content == "" {
		return "", "", "", false
	}
	at := out.UpdatedAt
	if at == "" {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	src := out.Source
	if src == "" {
		src = "remediation-runner"
	}
	return out.Content, src, at, true
}

func (h *Handler) readLocal() (string, string, string, bool) {
	for _, path := range h.localPaths {
		if path == "" {
			continue
		}
		if !strings.HasPrefix(path, "/") {
			// relative to cwd when platform-api runs
			if cwd, err := os.Getwd(); err == nil {
				candidate := filepath.Join(cwd, path)
				if fileOK(candidate) {
					return readFile(candidate)
				}
			}
			continue
		}
		if fileOK(path) {
			return readFile(path)
		}
	}
	return "", "", "", false
}

func fileOK(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func readFile(path string) (string, string, string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", false
	}
	info, _ := os.Stat(path)
	at := ""
	if info != nil {
		at = info.ModTime().UTC().Format(time.RFC3339)
	}
	return string(raw), path, at, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
