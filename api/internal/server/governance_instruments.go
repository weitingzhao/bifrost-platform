package server

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/instruments"
)

// GET /api/v1/governance/instruments (instruments.Route); this file hands it
// Gitea, once.
func (s *Server) handleInstruments(w http.ResponseWriter, r *http.Request) {
	instruments.Route{Service: func() *instruments.Service {
		if s.delivery == nil {
			return nil
		}
		s.instrumentsOnce.Do(func() {
			s.unitTreesOf()
			s.instruments = instruments.New(instruments.GiteaSource{Trees: s.unitTrees, Head: s.giteaMainHead})
		})
		return s.instruments
	}}.ServeHTTP(w, r)
}
