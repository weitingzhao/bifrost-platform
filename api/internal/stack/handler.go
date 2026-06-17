package stack

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

type Handler struct {
	svc *Service
}

func NewHandler(cfg *config.Config) *Handler {
	entry := cfg.DefaultCluster()
	return &Handler{svc: NewService(entry)}
}

func (h *Handler) HandleAddons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.Addons(r.Context()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
