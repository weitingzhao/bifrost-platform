package marketdata

import (
	"encoding/json"
	"net/http"

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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
