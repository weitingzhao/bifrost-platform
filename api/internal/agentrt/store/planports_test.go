//bifrost:owner plan

package store_test

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The store binds the plan domain's rule ports at its start. This test holds
// that binding to the rule store from outside the plan, through what the plan
// offers. The other ports' tests are in agentrt/integration.

// The stream a recorder log goes to.
const recorderStreamPrefix = "legacy:recorder:"

// preparedLog is a recorder log as lineage hands it to the plan, with the
// card body the test wants asked about; nothing of it is read from a store.
func preparedLog(work, body string) planstore.RecorderLog {
	return planstore.RecorderLog{Work: work, Stream: recorderStreamPrefix + work, LogID: noSuch, Doc: []byte(`{"fixture":"` + work + `"}`),
		CardBody:  func(string, string, string) string { return body },
		Imported:  func(context.Context, pgx.Tx) (int64, string, error) { return 0, "", nil },
		TryImport: func(context.Context, pgx.Tx) error { return nil }}
}

// lastCard is the card the asker was last asked.
func lastCard(t *testing.T, a *asker) planstore.Card {
	t.Helper()
	if len(a.cards) == 0 {
		t.Fatal("no card was asked")
	}
	return a.cards[len(a.cards)-1]
}

// Origin: 2026-11-17, plan core wall (design duty/from-biz-2026-11-17-plan-core-wall.md): the rules' side binds the two rule ports at the package's start (rules.go); this holds that binding in place and that each port answers what the rule store itself answers, with a rule in force and with none. 2026-11-18 (TD-582): read through what the rules decide in a proposal (the standing authorization and its "by", a card's ByRule), so the ports' functions need not be exported.
func TestRulePortsAreBoundToTheRuleStore(t *testing.T) {
	t.Parallel()
	direct := func(w *world) (plan, auto *store.StoredRule) {
		t.Helper()
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			if plan, err = store.CurrentRule(ctx, tx, rules.KindPlan, time.Now()); err != nil {
				return err
			}
			auto, err = store.CurrentRule(ctx, tx, rules.KindCardAuto, time.Now())
			return err
		}))
		return plan, auto
	}
	a := &asker{}
	// A first done rule naming the step's own task: what the plan-standing
	// rule grants, and only it.
	doneRule := func(w *world, key string) map[string]any {
		t.Helper()
		out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []planstore.PlanChange{{Op: "set_done_rule", Node: s2,
			Rule: []byte(`{"task_accepted":"t_01M5192P0JPMJAP63ZKRPXJ2VV"}`)}}, a.ask)
		ok(t, err)
		return out
	}
	// Whether each kind of card is asked by rule, against the kinds the rule
	// store has in force: one kind a rule can list, one it does not here.
	cardsAskedAsTheRuleSays := func(w *world, key string, kinds []string) {
		t.Helper()
		_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "rename", []planstore.PlanChange{{Op: "retitle", Node: item, Title: key}}, a.ask)
		ok(t, err)
		if got, want := lastCard(t, a).ByRule, slices.Contains(kinds, "plan_change"); got != want {
			t.Fatalf("%s: a plan change's card asked by rule %v; the rule store's kinds %v", key, got, kinds)
		}
		_, err = planstore.ProposeRecorderImport(ctx, w.rt, wr("thread:s1"), key, "retire", preparedLog(key, "a body"), a.ask)
		ok(t, err)
		if got, want := lastCard(t, a).ByRule, slices.Contains(kinds, "recorder_import"); got != want {
			t.Fatalf("%s: a recorder import's card asked by rule %v; the rule store's kinds %v", key, got, kinds)
		}
	}
	// No rule in force: the ports say none, as the rule store does; a port
	// that refused would fail the proposals.
	w := newWorld(t)
	if plan, auto := direct(w); plan != nil || auto != nil {
		t.Fatalf("with no rule: %+v %+v", plan, auto)
	}
	if out := doneRule(w, "none"); out["decision"] != "" {
		t.Fatalf("with no rule in force, decided at once: %v", out)
	}
	cardsAskedAsTheRuleSays(w, "no-rule", nil)
	// A rule in force that does not grant done rules grants nothing.
	putPlanStanding(t, w, false, false)
	if plan, _ := direct(w); plan == nil || plan.Plan.DoneRules {
		t.Fatalf("the fixture rule: %+v", plan)
	}
	if out := doneRule(w, "not-granted"); out["decision"] != "" {
		t.Fatalf("under a rule without done_rules, decided at once: %v", out)
	}
	// Rules in force: the decision names the rule the store has, and cards
	// are asked by rule for the kinds it lists.
	w = newWorld(t)
	// The whole list of kinds, as the rule store has it, including kinds the
	// plan never asks about: what follows through the plan sees only its own
	// three. 2026-11-18, TD-582: the list comparison this test had before it
	// went through the plan's contract.
	kindsInForce := func() []string {
		t.Helper()
		var kinds []string
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			var err error
			var r *store.StoredRule
			r, err = store.InForceIn(ctx, tx, rules.KindCardAuto)
			if r != nil {
				kinds = r.CardAuto.Kinds
			}
			return err
		}))
		return kinds
	}
	if kinds := kindsInForce(); kinds != nil {
		t.Fatalf("with no rule in force, the card-auto port: %v", kinds)
	}
	putPlanStanding(t, w, true, false)
	signCardAutoSeconds(t, w.rt, "5", "[blocked, mission, plan_change]")
	plan, auto := direct(w)
	if kinds := kindsInForce(); auto == nil || !reflect.DeepEqual(kinds, auto.CardAuto.Kinds) || !slices.Contains(kinds, "blocked") || !slices.Contains(kinds, "mission") {
		t.Fatalf("the card-auto port: %v; the rule store: %+v", kinds, auto)
	}
	if plan == nil || auto == nil || len(auto.CardAuto.Kinds) == 0 || !plan.Plan.DoneRules {
		t.Fatalf("the fixture rules are not in force: %+v %+v", plan, auto)
	}
	if out := doneRule(w, "granted"); out["decision"] != "applied" {
		t.Fatalf("under the rule, a first done rule: %v", out)
	}
	var by string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'by' FROM agentrt.event WHERE event_type = 'plan_change_decided' ORDER BY event_id DESC LIMIT 1`).Scan(&by))
	if want := fmt.Sprintf("standing authorization (%s v%d)", plan.ID, plan.Version); by != want {
		t.Fatalf("decided by %q; the rule store has %q", by, want)
	}
	cardsAskedAsTheRuleSays(w, "listed", auto.CardAuto.Kinds)
	// The third kind the plan asks about, a plan import, with no rule, under a
	// rule that does not list it and under one that does (a database each: one
	// import of a plan waits at a time).
	for _, c := range []struct {
		kinds string
		want  bool
	}{{"", false}, {"[blocked]", false}, {"[plan_import]", true}} {
		pool, req := importDB(t)
		if c.kinds != "" {
			signCardAutoSeconds(t, pool, "5", c.kinds)
		}
		cards := &asker{}
		_, err := planstore.ProposePlanImport(ctx, pool, wr("thread:s1"), "import", "import", req, cards.ask)
		ok(t, err)
		if got := lastCard(t, cards).ByRule; got != c.want {
			t.Fatalf("under kinds %q: a plan import's card asked by rule %v", c.kinds, got)
		}
	}
}
