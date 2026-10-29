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
			out = append(out, store.LandingGrantRequest{Op: op, ApprovalID: a.ID, ApprovalNumber: int64(a.Number), Status: a.Status, ParamsHash: a.ParamsHash, SeenAt: seen})
		}
		return out, nil
	}
}
