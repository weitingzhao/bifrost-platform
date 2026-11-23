package server

import (
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
)

// Mode on of "the platform releases PROD in batches" (headrelease/prod_shadow.go
// says why, headrelease/prod_release.go when): its one act. A release is
// started the way head-release starts STG's (startHeadRelease: the delivery
// service starts the run only when no run of the pipeline is under way and
// the release window is free, and holds the window for the run), under its
// own name. prod_batch_shadow.go, the shadow's reads, stays without it.

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
