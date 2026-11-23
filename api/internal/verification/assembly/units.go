package assembly

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/ciconfig"
	"github.com/weitingzhao/bifrost-platform/api/internal/unitexec"
)

// GET /api/v1/units/config (ciconfig.Route) reads the configuration through
// UnitConfig; server hands it Gitea's trees. Moved out of server
// (units_config.go) on 2026-11-23, unchanged.

// UnitConfig is how CI is set up for repo at commit, read from trees.
// Storage and HTTP remain adapters; CI configuration projection owns its model.
func UnitConfig(ctx context.Context, trees unitexec.TreesFunc, repo, commit string) (ciconfig.Answer, error) {
	return ciconfig.Read(ctx, ciconfig.Trees(trees), repo, commit)
}
