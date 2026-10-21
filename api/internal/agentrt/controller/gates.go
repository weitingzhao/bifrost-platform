package controller

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Gates is one pass of the architecture table's gates (store.RunGates,
// TD-397): in Env alone, on the deployments of GateRepo, the deployment read
// with the per-read timeout. Run after Facts, so a deployment just recorded
// is the one dispatched for; the end of what cannot run does not depend on
// that read.
func (c *Controller) Gates(ctx context.Context) (int, error) {
	if c.Env == "" || c.GateRepo == "" || c.Sources.Deployed == nil {
		return 0, nil
	}
	return store.RunGates(ctx, c.DB, c.Env, c.GateRepo, c.boundedDeployed)
}
