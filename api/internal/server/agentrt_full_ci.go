package server

import (
	"log/slog"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	verificationassembly "github.com/weitingzhao/bifrost-platform/api/internal/verification/assembly"
)

// wireFullCI gives PROD's controller the daily full CI (agentrt/fullci,
// built by verification/assembly): the landing CI's client creates and lists
// the runs, the platform's read-only Gitea identity reads main.
// AGENTRT_FULL_CI=off, another environment or no cluster wires none.
func (s *Server) wireFullCI(c *controller.Controller) {
	on, at, param, err := fullci.FullCISettings(os.Getenv)
	if err != nil {
		slog.Error("agentrt full CI not started", "error", err)
	}
	if !on || c.Env != "prod" || c.DB == nil || s.cluster == nil {
		return
	}
	ci := s.landingCIClient()
	ci.FullScopeParam = param
	// The full CI's day is the Owner's, in the zone of the briefs.
	c.FullCIZone = planpace.BriefZone
	// The PROD head-release loop starts a candidate's full CI with the same
	// runner when that is all the candidate waits on (headrelease fullCIFor).
	s.fullCIRun = verificationassembly.FullCIRun(ci, planpace.BriefZone)
	c.FullCI = verificationassembly.NewFullCI(ci, at)
}
