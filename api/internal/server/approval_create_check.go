package server

import (
	"context"
	"errors"
	"slices"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

// approvalCreateCheck runs before an approval row is written: a pipeline run
// whose signed commits Gitea does not have is refused then, not after the
// Owner approved it (TD-420, approval #80).
func (s *Server) approvalCreateCheck(ctx context.Context, action string, params map[string]any) error {
	if action == "void_exam_paper" && text(params["environment"]) != "prod" {
		return errors.New("void_exam_paper is PROD only until cross-environment runtime execution is available; " + approvalhome.Required)
	}
	if action == "mark_missions_no_merge" {
		_, err := s.noMergeRequest(params)
		return err
	}
	if slices.Contains(store.GrantActions, action) {
		return s.landingGrantCreateCheck(ctx, action, params)
	}
	if action != "start_pipeline_run" || s.delivery == nil {
		return nil
	}
	return s.delivery.CheckRequestRefs(ctx, params)
}
