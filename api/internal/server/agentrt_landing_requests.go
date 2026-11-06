package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (s *Server) wireLandingRequests(c *controller.Controller) {
	if c.Env != "prod" || s.approvals == nil {
		return
	}
	c.Reconcile.Requests = func(ctx context.Context) ([]store.LandingGrantRequest, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		list, err := s.approvals.Observations()
		if err != nil {
			return nil, err
		}
		seen := store.DecisionAt()
		var out []store.LandingGrantRequest
		for _, a := range list {
			if a.Drill || a.Tier != "D" || (a.Action != "approve_guardrail_landing" && a.Action != "approve_core_landing") || a.Params["env"] != "prod" || a.Params["repo"] != "bifrost-platform" {
				continue
			}
			op, _ := a.Params["op"].(string)
			if op == "" {
				continue
			}
			r, _ := a.Params["R"].(string)
			out = append(out, store.LandingGrantRequest{Op: op, R: r, ApprovalID: a.ID, ApprovalNumber: int64(a.Number), Status: a.Status, ParamsHash: a.ParamsHash, SeenAt: seen})
		}
		return out, nil
	}
	// The platform asks for the Owner's card itself once an operation waits
	// on a grant (R5:5.7 goal 3); the create check runs as for any request.
	c.Reconcile.Grants = s.landingGrantSources
	c.Reconcile.RequestGrant = func(ctx context.Context, action string, params map[string]any, reason string) (string, error) {
		a, err := s.approvals.Request(ctx, "controller", action, reason, "Reject the card: nothing lands without a grant.", params)
		return a.ID, err
	}
	// A pending card whose operation landed or ended without it is taken
	// back, not left on the Owner's list until it expires.
	c.Reconcile.SettleGrant = func(_ context.Context, id, reason string) error {
		return s.approvals.Withdraw(id, reason)
	}
}
