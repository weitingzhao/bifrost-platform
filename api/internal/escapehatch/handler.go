package escapehatch

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc   *Service
	audit *actuation.AuditLog
}

func NewHandler(cfg *config.Config, audit *actuation.AuditLog) *Handler {
	store := NewStore(cfg.ConfigDir())
	return &Handler{svc: NewService(cfg, store), audit: audit}
}

func (h *Handler) HandleGet(w http.ResponseWriter, r *http.Request) {
	resp, err := h.svc.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleRecordDrill(w http.ResponseWriter, r *http.Request) {
	var req RecordDrillRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	principal := actuation.PrincipalFromContext(r.Context())
	by := strings.TrimSpace(req.By)
	if by == "" {
		by = principal.Name
	}
	rec, err := h.svc.RecordDrill(by, req.Notes, req.RouteIDs)
	status := "ok"
	msg := "escape hatch quarterly drill recorded"
	if err != nil {
		status = "failed"
		msg = err.Error()
	}
	if h.audit != nil {
		h.audit.Record(r, "escape_hatch.drill", "platform-escape-hatch", status, msg)
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"drill": rec,
	})
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(v)
}
