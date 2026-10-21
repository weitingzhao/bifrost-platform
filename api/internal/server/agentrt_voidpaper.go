package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// voidExamPaper executes an approved void_exam_paper (TD-407; the Owner
// 2026-10-20: 「加一个只有我能用的『作废一题没答的卷』，只改平台、不碰题目和评分」):
// the paper of run_id, which no run was handed in for, is voided on this
// platform's runtime and the void records the approval's id. The action is
// tier D, approved on the Console only; it has no route of its own, so this
// executor is its only path. A request naming another environment's runtime
// is refused here: each platform voids only the papers its runtime issued.
func (s *Server) voidExamPaper(ctx context.Context, params map[string]any) (any, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return nil, errors.New("void_exam_paper runs only as an approval's execution")
	}
	rt := s.agentRuntime
	if rt == nil || rt.api == nil || rt.api.DB == nil {
		return nil, errors.New("this platform runs no agent runtime: no paper to void")
	}
	if env := text(params["environment"]); rt.env == "" || env != rt.env {
		return nil, fmt.Errorf("this platform runs the %q runtime, not %q: request the void on the platform that issued the paper", rt.env, env)
	}
	return store.VoidExamPaper(ctx, rt.api.DB, text(params["run_id"]), text(params["reason"]), att.ApprovalID)
}
