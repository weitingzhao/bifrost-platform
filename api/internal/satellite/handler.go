package satellite

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
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

func (h *Handler) HandleBusDeep(w http.ResponseWriter, r *http.Request) {
	if h.cfg == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "config unavailable",
		})
		return
	}

	envID := r.URL.Query().Get("env")
	if envID != "" {
		resp, err := h.svc.BusDeep(r.Context(), envID)
		if err != nil {
			if errors.Is(err, ErrUnknownEnvironment) {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": err.Error(),
				})
				return
			}
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Serialize per-env probes: parallel env fan-out storms Traefik NodePorts
	// (dev/stg/prod share the same LAN ingress) and yields false IB consumer downs.
	results := make([]BusDeepResponse, len(h.cfg.Environments))
	for i, env := range h.cfg.Environments {
		resp, err := h.svc.BusDeep(r.Context(), env.ID)
		if err != nil {
			results[i] = BusDeepResponse{
				Environment:  env.ID,
				Reachability: probe.ReachFail,
				Detail:       err.Error(),
			}
			continue
		}
		results[i] = resp
	}
	writeJSON(w, http.StatusOK, map[string]any{"buses": results})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
