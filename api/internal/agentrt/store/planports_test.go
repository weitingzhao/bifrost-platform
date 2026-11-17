//bifrost:owner plan

package store_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrttest"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// refused holds an error to the refusal of an unbound port.
func refused(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, store.ErrPortUnbound) {
		t.Fatalf("%s, unbound: %v; want ErrPortUnbound", what, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the plan code reads the rules in force through a port, and a port nobody bound must refuse the proposal or the card, never answer "no rule in force" (which would send every change to the Owner's card, or ask every card of the Owner, with nothing saying why).
func TestUnboundRulePortsRefuse(t *testing.T) {
	t.Parallel()
	for name, p := range map[string]*store.RulePorts{"nothing bound": nil, "an empty binding": {}} {
		r, err := store.PlanRuleViaForTest(ctx, p, nil)
		refused(t, name+": the plan-standing rule", err)
		if r != nil {
			t.Fatalf("%s: a rule with the refusal: %+v", name, r)
		}
		kinds, err := store.CardAutoKindsViaForTest(ctx, p, nil)
		refused(t, name+": the card-auto-answer rule", err)
		if kinds != nil {
			t.Fatalf("%s: kinds with the refusal: %v", name, kinds)
		}
	}
	// The positive control: a bound port is called and its answer returned.
	want := &store.PlanRule{ID: "plan-standing", Version: 7, DoneRules: true}
	bound := &store.RulePorts{
		PlanRuleInForce:      func(context.Context, pgx.Tx) (*store.PlanRule, error) { return want, nil },
		CardAutoKindsInForce: func(context.Context, pgx.Tx) ([]string, error) { return []string{"blocked"}, nil },
	}
	if r, err := store.PlanRuleViaForTest(ctx, bound, nil); err != nil || r != want {
		t.Fatalf("a bound port: %+v %v", r, err)
	}
	if kinds, err := store.CardAutoKindsViaForTest(ctx, bound, nil); err != nil || !reflect.DeepEqual(kinds, []string{"blocked"}) {
		t.Fatalf("a bound port: %v %v", kinds, err)
	}
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the rules' side binds the two rule ports at the package's start (envelope.go); this holds that binding in place and that each port answers what the rule store itself answers, with a rule in force and with none.
func TestRulePortsAreBoundToTheRuleStore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	read := func() (plan *store.PlanRule, kinds []string, direct [2]*store.StoredRule) {
		t.Helper()
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			if plan, err = store.PlanRuleInForceForTest(ctx, tx); err != nil {
				return err
			}
			if kinds, err = store.CardAutoKindsInForceForTest(ctx, tx); err != nil {
				return err
			}
			if direct[0], err = store.CurrentRule(ctx, tx, rules.KindPlan, time.Now()); err != nil {
				return err
			}
			direct[1], err = store.CurrentRule(ctx, tx, rules.KindCardAuto, time.Now())
			return err
		}))
		return plan, kinds, direct
	}
	// No rule in force: the ports say none, as the rule store does.
	if plan, kinds, direct := read(); plan != nil || kinds != nil || direct[0] != nil || direct[1] != nil {
		t.Fatalf("with no rule: %+v %v %+v", plan, kinds, direct)
	}
	putPlanStanding(t, w, true, false)
	_, err := w.rt.Exec(ctx, agentrttest.CardAutoRule)
	ok(t, err)
	plan, kinds, direct := read()
	if direct[0] == nil || direct[1] == nil || len(direct[1].CardAuto.Kinds) == 0 || !direct[0].Plan.DoneRules {
		t.Fatalf("the fixture rules are not in force: %+v", direct)
	}
	if plan == nil || *plan != (store.PlanRule{ID: direct[0].ID, Version: direct[0].Version, DoneRules: direct[0].Plan.DoneRules}) {
		t.Fatalf("the plan-standing port: %+v; the rule store: %+v", plan, direct[0])
	}
	if !reflect.DeepEqual(kinds, direct[1].CardAuto.Kinds) {
		t.Fatalf("the card-auto-answer port: %v; the rule store: %v", kinds, direct[1].CardAuto.Kinds)
	}
}
