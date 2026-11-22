package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	deliveryassembly "github.com/weitingzhao/bifrost-platform/api/internal/delivery/assembly"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
)

// mountDelivery hands the delivery domain's routes (delivery/assembly, since
// 2026-11-19) what the platform lends them: the gates, the domain's handlers
// server holds, and the answers server gives (the release policy's among
// them, release_policy_wire.go).
func (s *Server) mountDelivery(r chi.Router) {
	deliveryassembly.Mount(r, deliveryassembly.Gates{
		Viewer:   s.auth.Require(actuation.RoleViewer),
		Operator: s.auth.Require(actuation.RoleOperator),
		Admin:    s.auth.Require(actuation.RoleAdmin),
		Guard:    s.guard,
	}, deliveryassembly.Handlers{
		Pipelines:            s.delivery,
		GitOps:               s.gitops,
		Releases:             s.releases,
		ReleasePolicy:        s.handleReleasePolicy,
		ReleasePolicyCheck:   s.handleReleasePolicyCheck,
		ReleasePolicyInstall: s.handleReleasePolicyInstall,
		ReleasePolicyStage:   s.handleReleasePolicyStage,
		ReleaseFreeze:        s.handleReleaseFreeze,
		ReleaseUnfreeze:      s.handleReleaseUnfreeze,
		Released:             s.handleReleased,
		ReleaseContext:       s.handleReleaseContext,
		AgentrtMigration:     s.handleAgentrtMigration,
		ProdJudgements:       headrelease.ProdJudgementsRoute{DB: s.recordDB},
	})
}
