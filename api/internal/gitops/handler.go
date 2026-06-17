package gitops

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
	entry := cfg.DefaultCluster()
	return &Handler{svc: NewService(entry), audit: audit}
}

func (h *Handler) HandleApps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Apps(r.Context()))
}

func (h *Handler) HandleSyncApp(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "application name required"})
		return
	}
	resp, err := h.svc.SyncApplication(r.Context(), name)
	status := "ok"
	if err != nil {
		status = "failed"
	}
	if h.audit != nil {
		h.audit.Record(r, resp.Action, resp.Target, status, resp.Message)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
