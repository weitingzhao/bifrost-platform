package server

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// GET /api/v1/units/full-ci (cirunview.FullCIRoute); this file hands it the
// runtime's database.
func (s *Server) handleUnitFullCI(w http.ResponseWriter, r *http.Request) {
	cirunview.FullCIRoute{DB: func() eventlog.DB {
		if s.agentRuntime == nil || s.agentRuntime.api == nil {
			return nil
		}
		return s.agentRuntime.api.DB
	}}.ServeHTTP(w, r)
}
