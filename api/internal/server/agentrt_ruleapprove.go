package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

// agentrt_rule_approve (R5 phase 2 step 3b; the Owner 2026-11-06 Q2 A): a
// tier D card, PROD only, that lets a proposed rule version changing only
// capacity fields take effect on the Owner's Console approval instead of his
// signature. The create check refuses a request whose draft does not exist,
// is not the rule's next version or changes anything but capacity fields;
// the executor reads the approval back and writes the version through
// store.ApproveRule, which checks the draft again under the rule's lock.

// RuleApproveAction is the action's catalog id.
const RuleApproveAction = "agentrt_rule_approve"

// ruleApproveRequest decodes and binds a request to this platform: exactly
// {env, rule_id, rule_version, text_artifact_id}, env prod, a PROD home and
// a PROD runtime.
func (s *Server) ruleApproveRequest(params map[string]any) (store.RuleApproval, error) {
	var a store.RuleApproval
	for k := range params {
		switch k {
		case "env", "rule_id", "rule_version", "text_artifact_id":
		default:
			return a, fmt.Errorf("%s takes env, rule_id, rule_version and text_artifact_id, not %s", RuleApproveAction, k)
		}
	}
	version, ok := wholeNumber(params["rule_version"])
	if !ok {
		return a, fmt.Errorf("%s: rule_version is a whole number", RuleApproveAction)
	}
	a = store.RuleApproval{RuleID: text(params["rule_id"]), Version: version, TextArtifactID: text(params["text_artifact_id"])}
	if !approvalhome.IsProd() || text(params["env"]) != "prod" {
		return a, errors.New(RuleApproveAction + " is PROD only: " + approvalhome.Required)
	}
	rt := s.agentRuntime
	if rt == nil || rt.api == nil || rt.api.DB == nil {
		return a, errors.New("this platform runs no agent runtime")
	}
	if rt.env != "prod" {
		return a, fmt.Errorf("this platform runs the %q runtime; %s is PROD only", rt.env, RuleApproveAction)
	}
	return a, nil
}

// wholeNumber reads an integer parameter as a request or a stored approval
// has it (int, a JSON number, or its text).
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), n >= math.MinInt32 && n <= math.MaxInt32
	case float64:
		return int(n), n == math.Trunc(n) && n >= math.MinInt32 && n <= math.MaxInt32
	case json.Number:
		i, err := strconv.Atoi(n.String())
		return i, err == nil
	}
	return 0, false
}

// ruleApproveCreateCheck refuses, before the approval row exists, a request
// the executor would refuse (Q2 A): the draft was proposed, is the rule's
// next version and changes only capacity fields within their ceilings.
func (s *Server) ruleApproveCreateCheck(ctx context.Context, params map[string]any) error {
	a, err := s.ruleApproveRequest(params)
	if err != nil {
		return err
	}
	_, err = store.CheckRuleApproval(ctx, s.agentRuntime.api.DB, a)
	return err
}

// ruleApproveChanges is the card's view of the draft (actions.Describe):
// each capacity field it changes, old and new.
func (s *Server) ruleApproveChanges(ctx context.Context, params map[string]any) ([]actions.RuleChange, error) {
	a, err := s.ruleApproveRequest(params)
	if err != nil {
		return nil, err
	}
	check, err := store.CheckRuleApproval(ctx, s.agentRuntime.api.DB, a)
	if err != nil {
		return nil, err
	}
	out := make([]actions.RuleChange, 0, len(check.Changes))
	for _, c := range check.Changes {
		out = append(out, actions.RuleChange{Field: c.Field, Old: changeValue(c.Old), New: changeValue(c.New)})
	}
	return out, nil
}

// changeValue is a field's value as a card line shows it.
func changeValue(v any) string {
	if v == nil {
		return "none"
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := canon.Encode(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// ruleApprove is the catalog executor: the approval read back (tier D,
// approved on the Console with its number confirmed, the parameters it was
// approved with, PROD's), then the version written by the Owner with this
// approval's id.
func (s *Server) ruleApprove(ctx context.Context, params map[string]any) (any, error) {
	if _, ok := actions.CreateAttemptFrom(ctx); !ok {
		return nil, errors.New(RuleApproveAction + " runs only as an approval's execution")
	}
	a, err := s.ruleApproveRequest(params)
	if err != nil {
		return nil, err
	}
	rec, err := s.grantApproval(ctx, RuleApproveAction, params)
	if err != nil {
		return nil, err
	}
	a.ID = rec.ID
	return store.ApproveRule(ctx, s.agentRuntime.api.DB, store.Writer{Actor: "owner"}, a)
}

// ruleApproveOpenKey: a rule has at most one open agentrt_rule_approve
// request at a time, whichever version it names.
func ruleApproveOpenKey(action string, params map[string]any) string {
	if action != RuleApproveAction {
		return ""
	}
	id, _ := params["rule_id"].(string)
	if id == "" {
		return ""
	}
	return "rule-approve:" + id
}
