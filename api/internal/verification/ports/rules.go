package ports

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// For the plan's rule ports (planstore.RulePorts, bound in
// agentrt/planwiring): the plan-standing rule and the card-auto-answer rule
// in force, as far as the plan code reads them. A rule that does not read
// counts as none.

// PlanRule is the plan-standing rule in force.
type PlanRule struct {
	ID        string
	Version   int
	DoneRules bool
}

// PlanRuleInForce is the plan-standing rule in force in tx; nil when none.
func PlanRuleInForce(ctx context.Context, tx pgx.Tx) (*PlanRule, error) {
	r, err := store.InForceIn(ctx, tx, rules.KindPlan)
	if err != nil || r == nil {
		return nil, err
	}
	return &PlanRule{ID: r.ID, Version: r.Version, DoneRules: r.Plan.DoneRules}, nil
}

// CardAutoKindsInForce are the card kinds the card-auto-answer rule in force
// in tx answers; nil when none is in force.
func CardAutoKindsInForce(ctx context.Context, tx pgx.Tx) ([]string, error) {
	r, err := store.InForceIn(ctx, tx, rules.KindCardAuto)
	if err != nil || r == nil {
		return nil, err
	}
	return r.CardAuto.Kinds, nil
}

// For the agent runtime API's error answer (server/agentrtapi_api.go fail).

// Unsigned says err is the rule store's refusal of a rule the Owner has not
// signed (answered 403 "unsigned").
func Unsigned(err error) bool { return errors.Is(err, store.ErrUnsigned) }

// For the approvals domain's approve_rule action (approvalrules.RuleApproveRuntime,
// bound in server/agentrt_ruleapprove.go).

// RuleApproval names the rule version a card approves.
type RuleApproval struct {
	RuleID         string
	Version        int
	TextArtifactID string
	ID             string
}

// RuleChange is one value the approved version changes.
type RuleChange struct {
	Field    string
	Old, New any
}

func storeRuleApproval(a RuleApproval) store.RuleApproval {
	return store.RuleApproval{RuleID: a.RuleID, Version: a.Version, TextArtifactID: a.TextArtifactID, ID: a.ID}
}

// RuleApprovalChanges checks the approval against the rule store and lists
// what the version changes.
func RuleApprovalChanges(ctx context.Context, db eventlog.DB, a RuleApproval) ([]RuleChange, error) {
	check, err := store.CheckRuleApproval(ctx, db, storeRuleApproval(a))
	if err != nil {
		return nil, err
	}
	out := make([]RuleChange, 0, len(check.Changes))
	for _, c := range check.Changes {
		out = append(out, RuleChange{Field: c.Field, Old: c.Old, New: c.New})
	}
	return out, nil
}

// ApproveRuleVersion records the approval in the rule store as w.
func ApproveRuleVersion(ctx context.Context, db eventlog.DB, w actor.Writer, a RuleApproval) (map[string]any, error) {
	return store.ApproveRule(ctx, db, w, storeRuleApproval(a))
}
