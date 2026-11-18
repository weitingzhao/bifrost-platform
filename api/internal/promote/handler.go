package promote

import (
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
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
