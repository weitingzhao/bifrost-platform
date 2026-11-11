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
	// The signed rule's infra allow list is not passed to the infra landing CI
	// (ci.InfraLandingAllow stays nil): the PipelineRun admission policy takes
	// only plain-character parameters of at most 256 (TD-435, TD-436), and a
	// JSON list of globs is neither, so every infra landing CI was refused
	// (2026-11-11). Infra's doc_policy reads its own copy of the rule today;
	// how the signed list reaches it is TD-537.
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
