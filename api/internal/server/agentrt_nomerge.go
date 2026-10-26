package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (s *Server) noMergeRequest(params map[string]any) (store.NoMergeRequest, error) {
	var req store.NoMergeRequest
	raw, err := json.Marshal(params)
	if err != nil {
		return req, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		return req, err
	}
	if err := req.Validate(); err != nil {
		return req, err
	}
	rt := s.agentRuntime
	if rt == nil || rt.api == nil || rt.api.DB == nil {
		return req, errors.New("this platform runs no agent runtime")
	}
	if req.Env != rt.env {
		return req, fmt.Errorf("this platform runs the %q runtime, not %q: request no_merge on that environment's platform", rt.env, req.Env)
	}
	return req, nil
}

// The catalog executor is the only write surface. request_action uses the
// existing approvals API; tier D requires the Owner's Console approval.
func (s *Server) markMissionsNoMerge(ctx context.Context, params map[string]any) (any, error) {
	att, ok := actions.CreateAttemptFrom(ctx)
	if !ok {
		return nil, errors.New("mark_missions_no_merge runs only as an approval's execution")
	}
	req, err := s.noMergeRequest(params)
	if err != nil {
		return nil, err
	}
	results, err := store.MarkMissionsNoMerge(ctx, s.agentRuntime.api.DB, req, att.ApprovalID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"env": req.Env, "approval_id": att.ApprovalID, "missions": results}, nil
}
