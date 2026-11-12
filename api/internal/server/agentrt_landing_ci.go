package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
)

// Called only after wireLandingDrill has checked the signed repo rule.
// An unsigned infra repository constructs no sources and reads no heads.
func (s *Server) wireInfraLandingCI(c *controller.Controller, ci *landingci.Client) {
	if c.DrillGit["bifrost-trade-infra"] == nil {
		return
	}
	heads := s.agentRuntimeMains()
	if heads == nil {
		return
	}
	c.Drill.InfraPlatformHead = func(ctx context.Context) (string, error) {
		return heads(ctx, "bifrost-platform")
	}
	c.Drill.InfraCI = func(ctx context.Context, o landing.Operation) (landing.CIRun, error) {
		evidence, err := ci.Read(ctx, infraPreparedKey(o), o.PreparedAt)
		return evidence.CIRun, err
	}
	c.Drill.EnsureInfraCI = func(ctx context.Context, o landing.Operation) error {
		return ci.Ensure(ctx, landing.InfraCIName(o.R, o.M0, o.PlatformRevision, o.Generation), infraPreparedKey(o))
	}
}

func infraPreparedKey(o landing.Operation) landingci.Key {
	return landingci.Key{Repo: "bifrost-trade-infra", Revision: o.R, Base: o.M0,
		Siblings: map[string]string{"bifrost-platform": o.PlatformRevision}, Generation: o.Generation}
}
