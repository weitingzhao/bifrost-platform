package controller

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The tuning rule (rules/tuning.go): a pass that reads a key of it reads the
// rule once, at its start (readTuning); what a loop's alert counts to
// (failures_to_alert) is the value the last pass read. Without a rule in
// force every key keeps the code's default.

// readTuning reads the tuning rule in force, keeps it for the loops and
// returns it; one that cannot be read leaves the last one read (the
// defaults before any).
func (c *Controller) readTuning(ctx context.Context) *rules.Tuning {
	t, err := store.ReadTuning(ctx, c.DB)
	if err != nil {
		c.logf("the tuning rule could not be read; the values read before hold: %v", err)
		return c.tuning.Load()
	}
	c.tuning.Store(t)
	return t
}

// tuned is the tuning rule the last pass read, nil (the defaults) before any.
func (c *Controller) tuned() *rules.Tuning { return c.tuning.Load() }
