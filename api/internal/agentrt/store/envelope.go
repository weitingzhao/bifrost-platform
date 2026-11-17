package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// The plan code reads the two rules it acts under through its ports
// (planstore, planports.go): the rule in force of each kind, as far as the plan code
// reads it. A rule that does not read counts as none there too (inForce).
func init() {
	planstore.BindPlanRules(planstore.RulePorts{
		PlanRuleInForce: func(ctx context.Context, tx pgx.Tx) (*planstore.PlanRule, error) {
			r, err := inForce(ctx, tx, rules.KindPlan)
			if err != nil || r == nil {
				return nil, err
			}
			return &planstore.PlanRule{ID: r.ID, Version: r.Version, DoneRules: r.Plan.DoneRules}, nil
		},
		CardAutoKindsInForce: func(ctx context.Context, tx pgx.Tx) ([]string, error) {
			r, err := inForce(ctx, tx, rules.KindCardAuto)
			if err != nil || r == nil {
				return nil, err
			}
			return r.CardAuto.Kinds, nil
		},
	})
}

// inForce is CurrentRule at the database's clock; a stored rule that does
// not read counts as none.
func inForce(ctx context.Context, tx pgx.Tx, id string) (*StoredRule, error) {
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	r, err := CurrentRule(ctx, tx, id, now)
	if errors.Is(err, errBadRule) {
		return nil, nil
	}
	return r, err
}
