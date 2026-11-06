package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// recordLandingGrantRequest is a variable so the tests of the grant request
// (landing_grant_request.go) can run a whole pass without a database.
var recordLandingGrantRequest = store.RecordLandingGrantRequest

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
		if _, err := recordLandingGrantRequest(ctx, c.DB, r); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.ApprovalID, err))
		}
	}
	if err := c.requestLandingGrants(ctx, requests); err != nil {
		errs = append(errs, err)
	}
	if err := c.settleLandingGrants(ctx, requests); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// settleLandingGrants withdraws the pending approvals of operations that no
// longer wait on a grant, so none is left on the Owner's list for an
// operation that landed without it. The next pass records their new status.
func (c *Controller) settleLandingGrants(ctx context.Context, requests []store.LandingGrantRequest) error {
	if c.Reconcile.SettleGrant == nil {
		return nil
	}
	s, err := grantRequestReads.state(ctx, c.DB, "bifrost-platform")
	if err != nil {
		return err
	}
	var errs []error
	for _, r := range store.SettledLandingGrantRequests(s, requests) {
		o := s.Operations[r.Op]
		reason := fmt.Sprintf("Landing operation %s no longer waits on a grant (%s", r.Op, o.Phase)
		if o.Outcome != "" {
			reason += ": " + o.Outcome
		}
		if err := c.Reconcile.SettleGrant(ctx, r.ApprovalID, reason+")."); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.ApprovalID, err))
		}
	}
	return errors.Join(errs...)
}
