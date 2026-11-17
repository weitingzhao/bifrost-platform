package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
)

// The plan code's ports: what the plan files (plan*.go, cardkinds.go, the
// step facts and sign-off, progress*.go) need of another domain, declared
// here as small function types over the bases' and the plan's own types. The
// domain that provides one binds it at start (a Bind function per providing
// domain, called from an init of the provider's side); the plan files name
// the port and not the other domain's package or files, so the plan code can
// leave this package for one of the plan domain alone.
//
// A port nobody bound refuses: its caller gets ErrPortUnbound and the
// operation fails. It never answers as if there were no rule, no claim or
// no import.

// ErrPortUnbound: the plan code needed something of another domain and
// nothing was bound to provide it, so the operation is refused.
var ErrPortUnbound = errors.New("store: a port of the plan code is not bound")

func unbound(what, bind string) error {
	return fmt.Errorf("%w: %s (%s was not called); the operation is refused", ErrPortUnbound, what, bind)
}

// PlanRule is what the plan code reads of the plan-standing rule in force:
// which version it is, and whether it grants done rules.
type PlanRule struct {
	ID        string
	Version   int
	DoneRules bool
}

// RulePorts is what the plan code needs of the signed rules (verification).
type RulePorts struct {
	// PlanRuleInForce is the plan-standing rule in force at the database's
	// clock; nil when none is.
	PlanRuleInForce func(ctx context.Context, tx pgx.Tx) (*PlanRule, error)
	// CardAutoKindsInForce is the kinds of card the card-auto-answer rule in
	// force at the database's clock answers; none when no such rule is.
	CardAutoKindsInForce func(ctx context.Context, tx pgx.Tx) ([]string, error)
}

var rulePorts atomic.Pointer[RulePorts]

// BindPlanRules binds the rules' side. It is called once, from the init of
// the rules' files (envelope.go).
func BindPlanRules(p RulePorts) { rulePorts.Store(&p) }

func planRuleInForce(ctx context.Context, tx pgx.Tx) (*PlanRule, error) {
	return planRuleVia(ctx, rulePorts.Load(), tx)
}

func planRuleVia(ctx context.Context, p *RulePorts, tx pgx.Tx) (*PlanRule, error) {
	if p == nil || p.PlanRuleInForce == nil {
		return nil, unbound("the plan-standing rule in force", "BindPlanRules")
	}
	return p.PlanRuleInForce(ctx, tx)
}

func cardAutoKindsInForce(ctx context.Context, tx pgx.Tx) ([]string, error) {
	return cardAutoKindsVia(ctx, rulePorts.Load(), tx)
}

func cardAutoKindsVia(ctx context.Context, p *RulePorts, tx pgx.Tx) ([]string, error) {
	if p == nil || p.CardAutoKindsInForce == nil {
		return nil, unbound("the card-auto-answer rule in force", "BindPlanRules")
	}
	return p.CardAutoKindsInForce(ctx, tx)
}
