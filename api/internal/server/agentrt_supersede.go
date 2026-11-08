package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

const supersedeAction = "supersede_agent_tasks"

func (s *Server) supersedeRequest(params map[string]any) (store.SupersedeRequest, error) {
	var r store.SupersedeRequest
	raw, err := json.Marshal(params)
	if err != nil {
		return r, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&r); err != nil {
		return r, err
	}
	if err = r.Validate(); err != nil {
		return r, err
	}
	if !approvalhome.IsProd() || s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil || s.agentRuntime.env != "prod" {
		return r, errors.New("supersede_agent_tasks requires the PROD runtime; " + approvalhome.Required)
	}
	return r, nil
}
func (s *Server) supersedeCreateCheck(ctx context.Context, params map[string]any) error {
	r, err := s.supersedeRequest(params)
	if err != nil {
		return err
	}
	return store.SupersedeTasks(ctx, s.agentRuntime.api.DB, r, "")
}
func (s *Server) supersedeTasks(ctx context.Context, params map[string]any) (any, error) {
	if _, ok := actions.CreateAttemptFrom(ctx); !ok {
		return nil, errors.New("supersede_agent_tasks runs only as an approval execution")
	}
	r, err := s.supersedeRequest(params)
	if err != nil {
		return nil, err
	}
	approval, err := s.grantApproval(ctx, supersedeAction, params)
	if err != nil {
		return nil, err
	}
	if err = store.SupersedeTasks(ctx, s.agentRuntime.api.DB, r, approval.ID); err != nil {
		return nil, err
	}
	return map[string]any{"approval_id": approval.ID, "superseded": r.Entries}, nil
}
