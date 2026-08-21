package analytics

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

func (h *Handler) HandleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Status(r.Context()))
}

// HandleAPIProxy proxies /api/v1/plugins/analytics/api/* → analytics-docs (:8061)/*.
// Streams HTML/binary (Elementary report) — unlike JSON-only plugin proxies.
func (h *Handler) HandleAPIProxy(w http.ResponseWriter, r *http.Request) {
	suffix := chi.URLParam(r, "*")
	if suffix == "" {
		suffix = "elementary_report.html"
	}
	pluginPath := "/" + strings.TrimPrefix(suffix, "/")
	resp, err := h.svc.Proxy(r, pluginPath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "analytics docs unreachable: " + err.Error(),
			"hint":  "Ensure analytics-docs is Running in plugin-market-data, or set ANALYTICS_DOCS_URL",
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for _, hname := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Cache-Control"} {
		if v := resp.Header.Get(hname); v != "" {
			w.Header().Set(hname, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		if strings.HasSuffix(pluginPath, ".html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
	}
	// Allow Console iframe embedding from same-origin Vite / platform console
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
