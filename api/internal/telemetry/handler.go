package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	cfg   *config.Config
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	return &Handler{
		cfg:   cfg,
		svc:   NewService(cfg),
		audit: audit,
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

// HandleAttentionMute — L2 mute for Observability Attention (operator).
// Audits always; best-effort Alertmanager silence when configured.
// Explicitly NOT a health fix — UI suppress + optional AM silence only.
func (h *Handler) HandleAttentionMute(w http.ResponseWriter, r *http.Request) {
	var req AttentionMuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.AttentionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "attention_id required"})
		return
	}
	resp := h.svc.CreateAttentionMute(r.Context(), req)
	status := "ok"
	if resp.Alertmanager == "error" {
		status = "partial"
	}
	detail := fmt.Sprintf(
		"signal=%s domain=%s env=%s am=%s silence=%s expires=%s",
		req.SignalLabel, req.Domain, req.Env, resp.Alertmanager, resp.SilenceID, resp.ExpiresAt.Format(time.RFC3339),
	)
	if h.audit != nil {
		h.audit.Record(r, "telemetry.attention_mute", req.AttentionID, status, detail)
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
