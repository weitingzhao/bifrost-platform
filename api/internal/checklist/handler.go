package checklist

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

type Handler struct {
	store     *Store
	audit     *actuation.AuditLog
	husbandry HusbandrySource
}

func NewHandler(configDir string, audit *actuation.AuditLog) *Handler {
	return &Handler{
		store: NewStore(configDir),
		audit: audit,
	}
}

// Store exposes the checklist signal cache for read-only consumers.
func (h *Handler) Store() *Store { return h.store }

func (h *Handler) HandleGetSignals(w http.ResponseWriter, r *http.Request) {
	resp, err := h.store.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if overlay := h.liveHusbandrySignals(r.Context()); len(overlay) > 0 {
		resp.Signals = mergeHusbandryOverlay(resp.Signals, overlay)
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
