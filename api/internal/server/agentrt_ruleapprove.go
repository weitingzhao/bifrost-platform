package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalrules"
	"github.com/weitingzhao/bifrost-platform/api/internal/ruleapprove"
)

// agentrt_rule_approve lives in package ruleapprove; this file hands it the
// runtime's store and the approval's read-back.

// RuleApproveAction is the action's catalog id.
const RuleApproveAction = ruleapprove.Action

func storeRuleApproval(r ruleapprove.Request) store.RuleApproval {
	return store.RuleApproval{RuleID: r.RuleID, Version: r.Version, TextArtifactID: r.TextArtifactID, ID: r.ID}
}

func (s *Server) ruleApproveRuntime() ruleapprove.Runtime {
	return ruleapprove.Runtime{
		Env: func() (string, bool) {
			rt := s.agentRuntime
			if rt == nil || rt.api == nil || rt.api.DB == nil {
				return "", false
			}
			return rt.env, true
		},
		Check: func(ctx context.Context, r ruleapprove.Request) ([]ruleapprove.Change, error) {
			check, err := store.CheckRuleApproval(ctx, s.agentRuntime.api.DB, storeRuleApproval(r))
			if err != nil {
				return nil, err
			}
			out := make([]ruleapprove.Change, 0, len(check.Changes))
			for _, c := range check.Changes {
				out = append(out, ruleapprove.Change{Field: c.Field, Old: c.Old, New: c.New})
			}
			return out, nil
		},
		Grant: func(ctx context.Context, params map[string]any) (string, error) {
			rec, err := approvalrules.GrantApproval(ctx, s.approvals, RuleApproveAction, params)
			return rec.ID, err
		},
		Approve: func(ctx context.Context, r ruleapprove.Request) (any, error) {
			return store.ApproveRule(ctx, s.agentRuntime.api.DB, actor.Writer{Actor: "owner"}, storeRuleApproval(r))
		},
	}
}

func (s *Server) ruleApproveCreateCheck(ctx context.Context, params map[string]any) error {
	return s.ruleApproveRuntime().CreateCheck(ctx, params)
}

func (s *Server) ruleApproveChanges(ctx context.Context, params map[string]any) ([]actions.RuleChange, error) {
	return s.ruleApproveRuntime().Changes(ctx, params)
}

// ruleApprove is the catalog executor (actions_wire.go names it).
func (s *Server) ruleApprove(ctx context.Context, params map[string]any) (any, error) {
	return s.ruleApproveRuntime().Execute(ctx, params)
}
