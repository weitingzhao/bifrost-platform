package stack

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
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

func (h *Handler) HandleAddons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Addons(r.Context()))
}

func (h *Handler) HandleInstallAddon(w http.ResponseWriter, r *http.Request) {
	h.handleAddonActuation(w, r, true)
}

func (h *Handler) HandleUpgradeAddon(w http.ResponseWriter, r *http.Request) {
	h.handleAddonActuation(w, r, false)
}

func (h *Handler) handleAddonActuation(w http.ResponseWriter, r *http.Request, install bool) {
	name := strings.TrimSpace(chi.URLParam(r, "name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "addon name required"})
		return
	}

	var resp cluster.ActuationResponse
	var err error
	if install {
		resp, err = h.svc.InstallAddon(name)
	} else {
		resp, err = h.svc.UpgradeAddon(name)
	}

	status := "ok"
	if err != nil || !resp.OK {
		status = "failed"
	}
	if h.audit != nil {
		h.audit.Record(r, resp.Action, resp.Target, status, resp.Message)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	if !resp.OK {
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
