package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// ReadingSourceCatalogStatus is the map's status as the workers keep it after
// each scan round (GET /catalog/status), the first source the plan's reading
// atom serves (2026-11-25: the map line's criteria R6:6.9 are its numbers).
const ReadingSourceCatalogStatus = "catalog.status"

// wirePlanReadings serves the plan's reading atom the documents this process
// can read; any other source is unknown, so a rule naming it never holds.
func (s *Server) wirePlanReadings() {
	planstore.SetReadings(s.planReading)
}

func (s *Server) planReading(_ context.Context, source string) ([]byte, bool) {
	switch source {
	case ReadingSourceCatalogStatus:
		raw, err := s.catalogScanner().KeptStatus()
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		return raw, true
	}
	return nil, false
}
