package flexquery

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

type Handler struct {
	svc *Service
}

func NewHandler(clusterSvc *cluster.Service) *Handler {
	return &Handler{svc: NewService(clusterSvc)}
}

func (h *Handler) Service() *Service { return h.svc }

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Status(r.Context()))
}

// HandleAPIProxy proxies /api/v1/plugins/flex-query/api/* → Plugin API (:8791)/*.
func (h *Handler) HandleAPIProxy(w http.ResponseWriter, r *http.Request) {
	suffix := chi.URLParam(r, "*")
	if suffix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing plugin API path after /plugins/flex-query/api/",
		})
		return
	}
	pluginPath := "/" + strings.TrimPrefix(suffix, "/")
	resp, err := h.svc.Proxy(r, pluginPath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "flex-query plugin API unreachable: " + err.Error(),
			"hint":  "Ensure flex-query-api is Running, or set FLEX_QUERY_API_URL (e.g. http://127.0.0.1:8791)",
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "failed reading plugin API response: " + err.Error(),
		})
		return
	}

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
		var kubeStatus struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
			Reason  string `json:"reason"`
		}
		if json.Unmarshal(body, &kubeStatus) == nil && kubeStatus.Kind == "Status" {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": kubeStatus.Message,
				"hint":  "Deploy flex-query-api in plugin-flex-query NS, or set FLEX_QUERY_API_URL for local Plugin API",
			})
			return
		}
	}

	ct := resp.Header.Get("Content-Type")
	isJSON := ct == "" || strings.Contains(ct, "json")

	if !isJSON || (len(body) > 0 && body[0] != '{' && body[0] != '[' && body[0] != '"') {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		if msg == "" {
			msg = "empty response"
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "flex-query plugin returned non-JSON: " + msg,
			"hint":  "Plugin API may not be deployed — apply k8s/base or set FLEX_QUERY_API_URL",
		})
		return
	}

	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
