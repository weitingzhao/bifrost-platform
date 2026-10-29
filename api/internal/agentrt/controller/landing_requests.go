package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// LandingRequestsPass reads approvals before entering PostgreSQL. Approval
// failures/unknowns are observations, never grant availability. One bad record
// does not prevent the remaining roots' rejection settlements.
func (c *Controller) LandingRequestsPass(ctx context.Context) error {
	if c.Env != "prod" || c.Reconcile == nil || c.Reconcile.Requests == nil {
		return nil
	}
	requests, err := c.Reconcile.Requests(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range requests {
		if _, err := store.RecordLandingGrantRequest(ctx, c.DB, r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.ApprovalID, err))
		}
	}
	return errors.Join(errs...)
}
