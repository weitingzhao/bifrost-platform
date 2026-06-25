package buildgate

import (
	"encoding/json"
	"net/http"
	"strings"

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

func (h *Handler) HandleListPhases(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.ListPhases())
}

func (h *Handler) HandleGetGate(w http.ResponseWriter, r *http.Request) {
	phase := strings.ToUpper(chi.URLParam(r, "phase"))
	writeJSON(w, http.StatusOK, h.svc.GetGate(phase))
}

func (h *Handler) HandleRunGate(w http.ResponseWriter, r *http.Request) {
	phase := strings.ToUpper(chi.URLParam(r, "phase"))
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunGate(phase, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"ok": false, "action": "buildgate.run", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type signoffRequest struct {
	Notes string `json:"notes"`
}

func (h *Handler) HandleSignoff(w http.ResponseWriter, r *http.Request) {
	phase := strings.ToUpper(chi.URLParam(r, "phase"))
	var req signoffRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.Signoff(phase, req.Notes, principal.Name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		msg := resp.Message
		if err != nil {
			msg = err.Error()
		}
		h.audit.Record(r, resp.Action, resp.Target, status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "action": "buildgate.signoff", "message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}
