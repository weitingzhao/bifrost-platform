package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentdeploy"
)

// approvalCreateCheck runs before an approval row is written: a pipeline run
// whose signed commits Gitea does not have is refused then, not after the
// Owner approved it (TD-420, approval #80).
func (s *Server) approvalCreateCheck(ctx context.Context, action string, params map[string]any) error {
	if action == agentdeploy.DeployAgentdAction {
		return agentdDeployCreateCheck(params)
	}
	if action == RuleApproveAction {
		return s.ruleApproveCreateCheck(ctx, params)
	}
	if action != "start_pipeline_run" || s.delivery == nil {
		return nil
	}
	return s.delivery.CheckRequestRefs(ctx, params)
}
