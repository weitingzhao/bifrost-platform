package homelayout

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Handlers serve the saved layout of the Home page (GET and PUT
// /api/v1/home/layout). They were the server's home_layout.go (decoupling
// item 5, A5, 2026-11-18); the server mounts them, gates the routes and hands
// them what they stand on: the agent runtime's database (nil when it is not
// connected) and who a request writes as.
type Handlers struct {
	DB  func() eventlog.DB
	Who func(*http.Request) (actor.Writer, error)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, ErrNotMigrated) {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// Get answers the latest saved layout.
func (h Handlers) Get(w http.ResponseWriter, r *http.Request) {
	db := h.DB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the agent runtime database is not connected"})
		return
	}
	saved, err := Latest(r.Context(), db)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

// Save writes a new version over the one the caller read (base_version).
func (h Handlers) Save(w http.ResponseWriter, r *http.Request) {
	db := h.DB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the agent runtime database is not connected"})
		return
	}
	var b struct {
		BaseVersion *int            `json:"base_version"`
		Layout      json.RawMessage `json:"layout"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBytes+1024)).Decode(&b); err != nil || b.BaseVersion == nil || len(b.Layout) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {base_version, layout}, within the size of a layout"})
		return
	}
	who, err := h.Who(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	saved, err := Save(r.Context(), db, who, *b.BaseVersion, b.Layout)
	switch {
	case errors.Is(err, ErrStale):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "version": saved.Version})
	case errors.Is(err, ErrNotMigrated):
		writeError(w, err)
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, saved)
	}
}
