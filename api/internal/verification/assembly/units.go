package assembly

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/ciconfig"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitplan"
)

// GET /api/v1/units/config (ciconfig.Route) reads the configuration through
// UnitConfig; server hands it Gitea's trees. Moved out of server
// (units_config.go) on 2026-11-23, unchanged.

// UnitConfig is how CI is set up for repo at commit, read from trees.
// Storage and HTTP remain adapters; CI configuration projection owns its model.
func UnitConfig(ctx context.Context, trees unitexec.TreesFunc, repo, commit string) (ciconfig.Answer, error) {
	return ciconfig.Read(ctx, ciconfig.Trees(trees), repo, commit)
}

// FollowRelease makes every unit plan this process builds plan a pin with
// advance stg at the commit release gives for that repository, with where it
// came from (TD-588: the daily unit run tests what STG ships, not a commit
// someone has to move by hand). The server lends the STG release's reader
// once at start.
func FollowRelease(release func(repo string) (commit, from string)) {
	unitplan.SetFollower(release)
}
