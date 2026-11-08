package server

import (
	"context"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

var recoverBatchPush = store.RecoverBatchPush

func (s *Server) pushRecoveryRequest(params map[string]any) (store.PushRecoveryParams, error) {
	p, err := store.DecodePushRecoveryParams(params)
	if err != nil {
		return p, err
	}
	if !approvalhome.IsProd() || s.agentRuntime == nil || s.agentRuntime.env != "prod" || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
		return p, errors.New("push recovery requires PROD runtime")
	}
	return p, nil
}
func (s *Server) pushRecoveryCreateCheck(ctx context.Context, params map[string]any) error {
	p, err := s.pushRecoveryRequest(params)
	if err != nil {
		return err
	}
	return recoverBatchPush(ctx, s.agentRuntime.api.DB, s.landingGrantSources(), p, "")
}
func (s *Server) pushRecoveryExecutor(ctx context.Context, params map[string]any) (any, error) {
	p, err := s.pushRecoveryRequest(params)
	if err != nil {
		return nil, err
	}
	approval, err := s.grantApproval(ctx, store.RecoverBatchPushAction, params)
	if err != nil {
		return nil, err
	}
	if err = recoverBatchPush(ctx, s.agentRuntime.api.DB, s.landingGrantSources(), p, approval.ID); err != nil {
		return nil, err
	}
	return map[string]any{"op": p.Op, "R": p.R, "ci_run": p.CIRun, "recovered": true}, nil
}
