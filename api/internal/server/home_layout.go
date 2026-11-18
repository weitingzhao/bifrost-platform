package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/homelayout"
)

// The Owner's arrangement of the Ops home's city (package homelayout has the
// contract): anyone who reads the Console reads it; only the Owner saves it.
func (s *Server) mountHomeLayout(r chi.Router) {
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/home/layout", s.handleHomeLayout)
	r.With(s.auth.RequireAny(actuation.RoleAdmin)).Put("/home/layout", s.guard("save_home_layout", s.handleHomeLayoutSave))
}

func writeHomeLayoutError(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if errors.Is(err, homelayout.ErrNotMigrated) {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) handleHomeLayout(w http.ResponseWriter, r *http.Request) {
	db := s.recordDB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the agent runtime database is not connected"})
		return
	}
	saved, err := homelayout.Latest(r.Context(), db)
	if err != nil {
		writeHomeLayoutError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) handleHomeLayoutSave(w http.ResponseWriter, r *http.Request) {
	db := s.recordDB()
	if db == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the agent runtime database is not connected"})
		return
	}
	var b struct {
		BaseVersion *int            `json:"base_version"`
		Layout      json.RawMessage `json:"layout"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, homelayout.MaxBytes+1024)).Decode(&b); err != nil || b.BaseVersion == nil || len(b.Layout) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body is {base_version, layout}, within the size of a layout"})
		return
	}
	who, err := agentrtapi.WriterOf(r)
	if err != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		return
	}
	saved, err := homelayout.Save(r.Context(), db, who, *b.BaseVersion, b.Layout)
	switch {
	case errors.Is(err, homelayout.ErrStale):
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "version": saved.Version})
	case errors.Is(err, homelayout.ErrNotMigrated):
		writeHomeLayoutError(w, err)
	case err != nil:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusOK, saved)
	}
}
