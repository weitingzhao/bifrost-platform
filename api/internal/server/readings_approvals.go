package server

import (
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// The approval readings (approvalreadings.Route), in the plan's brief zone.
func (s *Server) handleApprovalReadings(w http.ResponseWriter, r *http.Request) {
	var records func() ([]approvals.Approval, error)
	if s.approvals != nil {
		records = s.approvals.Observations
	}
	approvalreadings.Route{Zone: planpace.BriefZone, Records: records}.ServeHTTP(w, r)
}
