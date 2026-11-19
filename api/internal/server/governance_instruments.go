package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/instruments"
)

// GET /api/v1/governance/instruments: the laws, the Skills and the MCP tools
// the source at main declares (package instruments), for the Crew page.

func (s *Server) handleInstruments(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "gitea access is not available on this platform"})
		return
	}
	s.instrumentsOnce.Do(func() {
		s.unitTreesOf()
		s.instruments = instruments.New(instruments.GiteaSource{Trees: s.unitTrees, Head: s.giteaMainHead})
	})
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	report, err := s.instruments.Read(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the instruments could not be read in time"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
