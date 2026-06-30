package migratewave

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	return &Handler{svc: NewService(cfg), audit: audit}
}

type actuationRequest struct {
	Notes string `json:"notes"`
}

func (h *Handler) HandleDeliver(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamId")
	waveID := chi.URLParam(r, "waveId")
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.MarkDelivered(streamID, waveID, principal.Name)
	h.recordActuation(r, resp.Action, resp.Target, err, resp.Message)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "migratewave.deliver", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSignoff(w http.ResponseWriter, r *http.Request) {
	streamID := chi.URLParam(r, "streamId")
	waveID := chi.URLParam(r, "waveId")
	var req actuationRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.Signoff(streamID, waveID, req.Notes, principal.Name)
	h.recordActuation(r, resp.Action, resp.Target, err, resp.Message)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "migratewave.signoff", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) recordActuation(r *http.Request, action, target string, err error, msg string) {
	if h.audit == nil {
		return
	}
	status := "ok"
	if err != nil {
		status = "failed"
		msg = err.Error()
	}
	if action == "" {
		action = "migratewave.actuation"
	}
	h.audit.Record(r, action, target, status, msg)
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}
