package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// grantedLandingWaiting reserves scheduling priority, not quota or push
// authority. The claim holds the stream lock, so approval/revocation and a
// new claim are ordered. Use the same database-only usability checks as
// UsableLandingGrant; the final gate still verifies and consumes the grant.
func grantedLandingWaiting(ctx context.Context, tx pgx.Tx, repo string) (bool, error) {
	if "landing:"+repo != landingRepoStream {
		return false, nil // Grants currently belong only to this stream.
	}
	r, err := scanLandingStream(ctx, tx, landingRepoStream)
	if err != nil || len(r.order) == 0 {
		return false, err
	}
	s, err := landing.FoldWithGrants(r.entries, grantPeriods(r.order))
	if err != nil {
		return false, err
	}
	now := DecisionAt()
	for id, o := range s.Operations {
		if o.Phase != "awaiting_grant" || s.LatestOperation(o.Task) != id {
			continue
		}
		if g := candidateGrant(r.order, id, o, now); g != nil {
			enabled, err := grantEnabled(ctx, tx, g.Tenant, g.Repo)
			if err != nil || enabled {
				return enabled, err
			}
		}
	}
	return false, nil
}
