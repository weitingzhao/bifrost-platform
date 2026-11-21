package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"github.com/weitingzhao/bifrost-platform/api/internal/headrelease"
)

// Mode on of "the platform releases PROD in batches" (headrelease/prod_shadow.go
// says why, headrelease/prod_release.go when): its one act. A release is
// started the way head-release starts STG's (startHeadRelease: the delivery
// service starts the run only when no run of the pipeline is under way and
// the release window is free, and holds the window for the run), under its
// own name. prod_batch_shadow.go, the shadow's reads, stays without it.

// prodRelease starts the PROD delivery of revision with params beside it.
func (s *Server) prodRelease(ctx context.Context, pipeline, revision string, params map[string]string) (string, string, error) {
	run, heldBy, err := s.startHeadRelease(ctx, headrelease.ProdReleasePrincipal, pipeline, revision, params)
	if err != nil {
		return "", "", err
	}
	if heldBy != "" {
		return "", "not started: " + heldBy, nil
	}
	return run.Name, "", nil
}

// prodReleaseZone is the briefs' zone, in which the release hours are read;
// nil (no hours) when it does not load.
func prodReleaseZone() *time.Location {
	loc, err := time.LoadLocation(planpace.BriefZone)
	if err != nil {
		slog.Error("prod release: the briefs' zone does not load; no release hours", "zone", planpace.BriefZone, "error", err)
		return nil
	}
	return loc
}
