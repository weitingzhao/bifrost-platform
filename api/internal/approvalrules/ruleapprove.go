// ruleapprove.go (was package ruleapprove until 2026-11-22, law 79) is the action agentrt_rule_approve (R5 phase 2 step 3b;
// the Owner 2026-11-06 Q2 A): a tier D card, PROD only, that lets a proposed
// rule version changing only capacity fields take effect on the Owner's
// Console approval instead of his signature. The create check refuses a
// request whose draft does not exist, is not the rule's next version or changes
// anything but capacity fields; the executor reads the approval back and writes
// the version through the store, which checks the draft again under the rule's
// lock. The store and the approval's read-back are handed in.
package approvalrules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actions"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
)

// RuleApproveAction is the action's catalog id.
const RuleApproveAction = "agentrt_rule_approve"

// RuleApproval names the draft a card approves; ID is the approval's, once read back.
type RuleApproval struct {
	RuleID         string
	Version        int
	TextArtifactID string
	ID             string
}

// RuleChange is one capacity field a draft changes.
type RuleChange struct {
	Field    string
	Old, New any
}

// RuleApproveRuntime is what the action stands on.
type RuleApproveRuntime struct {
	// Env is the environment of this platform's agent runtime; ok is false
	// when it runs none.
	Env func() (env string, ok bool)
	// Check checks the draft, writing nothing, and answers what it changes.
	Check func(ctx context.Context, r RuleApproval) ([]RuleChange, error)
	// Grant reads back the approval this execution is for, and its id.
	Grant func(ctx context.Context, params map[string]any) (string, error)
	// Approve writes the version as the Owner, with the approval's id.
	Approve func(ctx context.Context, r RuleApproval) (any, error)
}

func text(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// request decodes and binds a request to this platform: exactly {env, rule_id,
// rule_version, text_artifact_id}, env prod, a PROD home and a PROD runtime.
func (rt RuleApproveRuntime) request(params map[string]any) (RuleApproval, error) {
	var a RuleApproval
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
	a = RuleApproval{RuleID: text(params["rule_id"]), Version: version, TextArtifactID: text(params["text_artifact_id"])}
	if !approvalhome.IsProd() || text(params["env"]) != "prod" {
		return a, errors.New(RuleApproveAction + " is PROD only: " + approvalhome.Required)
	}
	env, ok := rt.Env()
	if !ok {
		return a, errors.New("this platform runs no agent runtime")
	}
	if env != "prod" {
		return a, fmt.Errorf("this platform runs the %q runtime; %s is PROD only", env, RuleApproveAction)
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

// CreateCheck refuses, before the approval row exists, a request the executor
// would refuse (Q2 A): the draft was proposed, is the rule's next version and
// changes only capacity fields within their ceilings.
func (rt RuleApproveRuntime) CreateCheck(ctx context.Context, params map[string]any) error {
	a, err := rt.request(params)
	if err != nil {
		return err
	}
	_, err = rt.Check(ctx, a)
	return err
}

// Changes is the card's view of the draft (actions.Describe): each capacity
// field it changes, old and new.
func (rt RuleApproveRuntime) Changes(ctx context.Context, params map[string]any) ([]actions.RuleChange, error) {
	a, err := rt.request(params)
	if err != nil {
		return nil, err
	}
	changes, err := rt.Check(ctx, a)
	if err != nil {
		return nil, err
	}
	out := make([]actions.RuleChange, 0, len(changes))
	for _, c := range changes {
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

// Execute is the catalog executor: the approval read back (tier D, approved on
// the Console with its number confirmed, the parameters it was approved with,
// PROD's), then the version written by the Owner with this approval's id.
func (rt RuleApproveRuntime) Execute(ctx context.Context, params map[string]any) (any, error) {
	if _, ok := actions.CreateAttemptFrom(ctx); !ok {
		return nil, errors.New(RuleApproveAction + " runs only as an approval's execution")
	}
	a, err := rt.request(params)
	if err != nil {
		return nil, err
	}
	id, err := rt.Grant(ctx, params)
	if err != nil {
		return nil, err
	}
	a.ID = id
	return rt.Approve(ctx, a)
}

// ruleApproveOpenKey: a rule has at most one open agentrt_rule_approve request at a time,
// whichever version it names.
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
