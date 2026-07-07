package telemetry

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	cfg *config.Config
	svc *Service
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{
		cfg: cfg,
		svc: NewService(cfg),
	}
}

func (h *Handler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	resp, err := h.svc.Overview(r.Context(), ns)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "missing query parameter q",
		})
		return
	}
	ns := r.URL.Query().Get("ns")
	resp, err := h.svc.Query(r.Context(), q, ns)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandlePromQL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	resp, err := h.svc.PromQL(r.Context(), q)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleAlerts(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Alerts(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleTargets(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	resp, err := h.svc.Targets(r.Context(), state)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPrometheusNotConfigured):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": err.Error(),
			"hint":  "Set observability_urls.prometheus in clusters.yaml or PLATFORM_PROMETHEUS_URL",
		})
	case errors.Is(err, ErrUnknownQuery):
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
	case errors.Is(err, ErrMissingPromQL):
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": err.Error(),
		})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": err.Error(),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
