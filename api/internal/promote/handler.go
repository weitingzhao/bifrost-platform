package promote

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/opscontext"
)

// Handler holds the persisted release-gate state that GET /context overlays.
type Handler struct {
	store *Store
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{store: NewStore(configDirFrom(cfg))}
}

func (h *Handler) Store() *Store {
	return h.store
}

func configDirFrom(cfg *config.Config) string {
	if cfg == nil || cfg.ConfigPath == "" {
		return "config"
	}
	return filepath.Dir(cfg.ConfigPath)
}

// HandleOpsContext answers the ops context with this store's promotions over it.
func (h *Handler) HandleOpsContext(base *opscontext.File) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if base == nil {
			writeOpsContextJSON(w, http.StatusInternalServerError, map[string]string{"error": "ops context not loaded"})
			return
		}
		writeOpsContextJSON(w, http.StatusOK, OverlayContext(base, h.store))
	}
}

func writeOpsContextJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
