package research

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(clusterSvc *cluster.Service, audit *actuation.AuditLog) *Handler {
	return &Handler{svc: NewService(clusterSvc), audit: audit}
}

func (h *Handler) Service() *Service { return h.svc }

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Status(r.Context()))
}

// HandleLegacyAnalyticsRedirect sends retired Plugin → Analytics URLs to Research API.
//
//	GET /plugins/analytics/status     → /api/v1/research/analytics/elementary
//	GET /plugins/analytics/api/{path} → /api/v1/research/analytics/elementary/files/{path}
func (h *Handler) HandleLegacyAnalyticsRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, legacyAnalyticsRedirectTarget(r.URL.Path), http.StatusPermanentRedirect)
}

func legacyAnalyticsRedirectTarget(requestPath string) string {
	const filesPrefix = "/api/v1/research/analytics/elementary/files/"
	path := strings.TrimSpace(requestPath)
	if strings.HasSuffix(path, "/plugins/analytics/status") {
		return "/api/v1/research/analytics/elementary"
	}
	_, rest, ok := strings.Cut(path, "/plugins/analytics/api/")
	if !ok {
		if strings.HasSuffix(path, "/plugins/analytics/api") {
			return filesPrefix + "elementary_report.html"
		}
		return "/api/v1/research/analytics/elementary"
	}
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		rest = "elementary_report.html"
	}
	return filesPrefix + rest
}

// HandleAPIProxy proxies:
//
//	/api/v1/research/*              → Research API (:8795)/*
//	/api/v1/plugins/research/api/*  → Research API (:8795)/*  (plugin-style alias)
//
// Example: GET .../research/health → GET {research}/health
// Example: GET .../research/analytics/sepa/criteria-stats → GET {research}/analytics/sepa/criteria-stats
func (h *Handler) HandleAPIProxy(w http.ResponseWriter, r *http.Request) {
	suffix := chi.URLParam(r, "*")
	if suffix == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing Research API path after /research/ or /plugins/research/api/",
		})
		return
	}
	upstreamPath := stripProxyPrefix(suffix)
	resp, err := h.svc.Proxy(r, upstreamPath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "research API unreachable: " + err.Error(),
			"hint":  "Ensure research-api is Running, or set RESEARCH_API_URL (e.g. http://127.0.0.1:8795)",
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "failed reading Research API response: " + err.Error(),
		})
		return
	}

	// K8s API proxy returns Status objects (e.g. service not found) — normalize for Console.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500 {
		var kubeStatus struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
			Reason  string `json:"reason"`
		}
		if json.Unmarshal(body, &kubeStatus) == nil && kubeStatus.Kind == "Status" {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": kubeStatus.Message,
				"hint":  "Deploy research-api in research NS, or set RESEARCH_API_URL for local Research API",
			})
			return
		}
	}

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}

func (h *Handler) HandleCronJobTrigger(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	resp, err := h.svc.TriggerCronJob(r.Context(), name)
	status := "ok"
	if err != nil || !resp.OK {
		status = "failed"
	}
	if h.audit != nil {
		h.audit.Record(r, "research.cronjob.trigger", resp.CronJob, status, resp.Message)
	}
	if err != nil || !resp.OK {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// stripProxyPrefix normalizes the chi wildcard suffix to an upstream path.
// Chi already strips the route prefix; this only ensures a leading slash.
func stripProxyPrefix(suffix string) string {
	return "/" + strings.TrimPrefix(suffix, "/")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
