package controller

import (
	"context"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
)

// The daily full CI's decisions are agentrt/fullci's (verification); the
// controller runs its loop and lends each pass what it holds (fullci.Host).

// startFullCI runs the loop when there is one, timed as the others are.
func (c *Controller) startFullCI(ctx context.Context, wg *sync.WaitGroup) {
	if c.FullCI == nil {
		return
	}
	c.every(ctx, wg, "agentrt-full-ci", fullci.FullCIEvery, func() error {
		passCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		_, err := c.FullCIPass(passCtx)
		return err
	})
}

// FullCIPass is one pass of the full CI loop (fullci.FullCI.Pass) with the
// controller's ledger, clock, log, the commit STG runs and the zone the
// assembly bound (FullCIZone: the zone of the briefs). It returns how many
// runs it started and red runs it noted.
func (c *Controller) FullCIPass(ctx context.Context) (int, error) {
	return c.FullCI.Pass(ctx, c.fullCIHost())
}

// fullCIHost binds what a full CI pass reads of the controller.
func (c *Controller) fullCIHost() fullci.Host {
	h := fullci.Host{DB: c.DB, Now: c.clock, Logf: c.logf, Zone: c.FullCIZone,
		Slow: func(start time.Time, format string, args ...any) { c.slow(slowStepLog, start, format, args...) }}
	if c.Sources.Deployed != nil {
		h.STG = func(ctx context.Context) (string, bool) {
			stg, _, ok := c.boundedDeployed(ctx, "stg")
			return stg, ok
		}
	}
	return h
}
