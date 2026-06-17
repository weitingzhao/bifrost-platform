package promote

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc   *Service
	store *Store
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	svc := NewService(cfg)
	return &Handler{svc: svc, store: svc.store, audit: audit}
}

func (h *Handler) Store() *Store {
	return h.store
}

func (h *Handler) HandleGetReleaseGate(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.LastGate(r.Context()))
}

func (h *Handler) HandleRunReleaseGate(w http.ResponseWriter, r *http.Request) {
	principal := actuation.PrincipalFromContext(r.Context())
	resp, err := h.svc.RunReleaseGate(r.Context(), principal.Name)
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
			"ok":      false,
			"action":  "promote.release-gate",
			"message": err.Error(),
		})
		return
	}
	code := http.StatusOK
	if !resp.OK {
		code = http.StatusOK // gate ran; result may be fail
	}
	writeJSON(w, code, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
