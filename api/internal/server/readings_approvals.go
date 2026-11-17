package server

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalreadings"
	"net/http"
	"time"
)

// The HTTP adapter keeps the existing record source, timezone and error responses.
func (s *Server) handleApprovalReadings(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform keeps no approval records"})
		return
	}
	loc, err := time.LoadLocation(planpace.BriefZone)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := s.approvals.Observations()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the approval records could not be read: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, approvalreadings.Compute(items, loc, time.Now()))
}
