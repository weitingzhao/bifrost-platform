package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	verificationassembly "github.com/weitingzhao/bifrost-platform/api/internal/verification/assembly"
)

// mountVerification hands the verification domain's routes
// (verification/assembly, since 2026-11-19) what the platform lends them:
// the gates and the handlers server answers with.
func (s *Server) mountVerification(r chi.Router) {
	verificationassembly.Mount(r, verificationassembly.Gates{
		Viewer:   s.auth.Require(actuation.RoleViewer),
		Operator: s.auth.Require(actuation.RoleOperator),
		Guard:    s.guard,
	}, verificationassembly.Handlers{
		EvidenceCommit:      s.handleEvidenceCommit,
		EvidenceCI:          s.handleEvidenceCI,
		UnitPlan:            s.handleUnitPlan,
		UnitScope:           s.handleUnitScope,
		UnitConfig:          s.handleUnitConfig,
		UnitFullCI:          s.handleUnitFullCI,
		UnitFullCICheck:     s.handleUnitFullCICheck,
		UnitRuns:            s.handleUnitRuns,
		CIScorecard:         s.handleCIScorecard,
		CIDomains:           s.handleCIDomains,
		CIChanges:           s.handleCIChanges,
		UnitEvidenceRequest: s.handleUnitEvidenceRequest,
	})
}
