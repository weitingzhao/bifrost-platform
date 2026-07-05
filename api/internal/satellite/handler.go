package satellite

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

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

	results := make([]BusDeepResponse, len(h.cfg.Environments))
	var wg sync.WaitGroup
	for i, env := range h.cfg.Environments {
		wg.Add(1)
		go func(idx int, envID string) {
			defer wg.Done()
			resp, err := h.svc.BusDeep(r.Context(), envID)
			if err != nil {
				results[idx] = BusDeepResponse{
					Environment:  envID,
					Reachability: probe.ReachFail,
					Detail:       err.Error(),
				}
				return
			}
			results[idx] = resp
		}(i, env.ID)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]any{"buses": results})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
