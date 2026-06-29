package hermesgateway

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Handler struct {
	httpClient *http.Client
}

func NewHandler() *Handler {
	return &Handler{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (h *Handler) gatewayURL() string {
	url := strings.TrimRight(strings.TrimSpace(os.Getenv("HERMES_GATEWAY_URL")), "/")
	if url == "" {
		return ""
	}
	return url
}

func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	url := h.gatewayURL()
	if url == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "not_configured",
			"error":  "HERMES_GATEWAY_URL not set",
		})
		return
	}
	h.proxy(w, r, url+"/health")
}

func (h *Handler) HandleSkills(w http.ResponseWriter, r *http.Request) {
	url := h.gatewayURL()
	if url == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"gateway_status": "not_configured",
			"skills":         []any{},
			"generated_at":   time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	h.proxy(w, r, url+"/skills")
}

func (h *Handler) HandleSchedules(w http.ResponseWriter, r *http.Request) {
	url := h.gatewayURL()
	if url == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"schedules":    []any{},
			"generated_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	h.proxy(w, r, url+"/schedules")
}

func (h *Handler) HandleExecutions(w http.ResponseWriter, r *http.Request) {
	url := h.gatewayURL()
	if url == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"executions":   []any{},
			"total":        0,
			"generated_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	target := url + "/executions"
	if q := r.URL.RawQuery; q != "" {
		target += "?" + q
	}
	h.proxy(w, r, target)
}

func (h *Handler) HandleSkillActuationLevel(w http.ResponseWriter, r *http.Request) {
	url := h.gatewayURL()
	if url == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "HERMES_GATEWAY_URL not set",
		})
		return
	}
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "actuation level update not yet implemented in gateway",
	})
}

func (h *Handler) proxy(w http.ResponseWriter, r *http.Request, target string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "hermes gateway unreachable: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
