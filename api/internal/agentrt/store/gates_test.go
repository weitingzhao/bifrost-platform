package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gaterun"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// The architecture table's gates run by the runtime (TD-397,
// CUT-td397-gate-runs-2026-10-20.md).

// gateOf is a gate on one package, its spec and sha256.
func gateOf(t *testing.T, id string, tests ...string) (gaterun.Gate, []byte, string) {
	t.Helper()
	g := gaterun.Gate{ID: id, Text: "gate " + id, Commands: []gaterun.Command{{Pkg: "./internal/agentrt/plan/", Tests: tests}}}
	body, sum, err := g.Spec()
	ok(t, err)
	return g, body, sum
}

// gateWorld is a kernel world with the standing envelope the gates run in
// (the deterministic executor on h1), each gate's spec stored, and the
// architecture rows with these rules.
func gateWorld(t *testing.T, specs [][]byte, rows ...[2]string) *world {
	t.Helper()
	w := newKernelWorld(t)
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, ruleText("standing-envelope:dev", 1, rules.Initial, since, until, `tenant: dev
max_action: branch
budget: {max_attempts: 40, max_wall_hours: 48}
executors: [exec:deterministic]
hosts: [h1, h2]
`))))
	for _, b := range specs {
		w.artifact(t, "task_spec", string(b))
	}
	w.arch(t, rows...)
	return w
}

func gateRule(env string, gates ...[2]string) string {
	var list []string
	for _, g := range gates {
		list = append(list, `{"test":"`+g[0]+`","spec_sha256":"`+g[1]+`"}`)
	}
	return `{"gates_passed":[` + strings.Join(list, ",") + `],"env":"` + env + `"}`
}

// The plan-change card recommends applying only the reviewed catalog's
// first gates for rows with no rule, in this environment; a rule answering
// it applies it only while that still holds (CUT §7② A).
func TestGatePlanChangeRecommendation(t *testing.T) {
	t.Parallel()
	row := gaterun.Catalog[0]
	var specs [][]byte
	for _, g := range row.Gates {
		body, _, err := g.Spec()
		ok(t, err)
		specs = append(specs, body)
	}
	_, otherSpec, otherSum := gateOf(t, row.Gates[0].ID, "TestGrammarIsClosed")
	specs = append(specs, otherSpec)
	label := "ARCH:" + row.Label
	w := gateWorld(t, specs, [2]string{label, ""}, [2]string{"ARCH:" + gaterun.Catalog[1].Label, ""})
	want, err := row.Rule("dev")
	ok(t, err)
	prodRule, err := row.Rule("prod")
	ok(t, err)
	a := &asker{}
	propose := func(key string, changes ...store.PlanChange) store.Card {
		t.Helper()
		n := len(a.cards)
		_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, "ARCH", "the catalog's gates", changes, a.ask)
		ok(t, err)
		if len(a.cards) != n+1 {
			t.Fatalf("%s: no card", key)
		}
		return a.cards[n]
	}
	set := func(node string, rule []byte) store.PlanChange {
		return store.PlanChange{Op: "set_done_rule", Node: node, Rule: json.RawMessage(rule)}
	}
	if c := propose("exact", set(label, want)); c.Recommended != 1 || !strings.Contains(c.RecommendedWhy, "the reviewed catalog's for dev") {
		t.Fatalf("the catalog's rule: %+v", c)
	}
	for key, changes := range map[string][]store.PlanChange{
		"another env":     {set(label, prodRule)},
		"another row":     {set("ARCH:"+gaterun.Catalog[1].Label, want)},
		"the row twice":   {set(label, want), set(label, want)},
		"mixed":           {set(label, want), {Op: "retitle", Node: label, Title: "x"}},
		"not the catalog": {set(label, []byte(gateRule("dev", [2]string{row.Gates[0].ID, otherSum})))},
	} {
		if c := propose(key, changes...); c.Recommended != 0 {
			t.Errorf("%s: recommended %d (%s)", key, c.Recommended, c.RecommendedWhy)
		}
	}
	// The rule answers the recommended card; a row given a rule meanwhile is
	// not overwritten: the change fails.
	w.setArchRule(t, label, string(prodRule))
	byRule := func(_ context.Context, id string) (store.Question, bool) {
		if id != "q_a" { // the first card, the catalog's rule
			return store.Question{ID: id, Status: "open"}, true
		}
		return store.Question{ID: id, Status: store.QuestionAnswered, Option: store.CardApplyKey, AnsweredBy: "rule card-auto-answer v3",
			AnsweredAt: "2026-10-21T00:00:00Z", Channel: "rule"}, true
	}
	if _, err := store.DecidePlanChanges(ctx, w.rt, byRule, a.ack); err != nil {
		t.Fatal(err)
	}
	var decision, why string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'decision', COALESCE(body_json->>'error', '') FROM agentrt.event WHERE event_type = 'plan_change_decided'
	  AND event_stream_id = 'plan:ARCH' ORDER BY event_id LIMIT 1`).Scan(&decision, &why))
	if decision != "failed" || !strings.Contains(why, "no longer the catalog's first gates") {
		t.Fatalf("decided %s: %s", decision, why)
	}
	if rule := w.ruleOf(t, label); rule != string(prodRule) {
		t.Fatalf("the row's rule became %s", rule)
	}
}

// setArchRule sets a row's rule as a plan change writes it.
func (w *world) setArchRule(t *testing.T, node, rule string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("ARCH"), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "set_done_rule"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, node, rule)
		return err
	}))
}

func (w *world) ruleOf(t *testing.T, node string) string {
	t.Helper()
	var doc string
	ok(t, w.rt.QueryRow(ctx, `SELECT done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = $1`, node).Scan(&doc))
	v, err := canon.Marshal([]byte(doc))
	ok(t, err)
	return string(v)
}

// 0007 (TD-397): a verify task with no subject goes in only as a gate task
// the controller makes: class gate, effect none, off the plan. An ordinary
// verify or review still needs its subject.
func TestGateTaskConstraint(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	try := func(actor string, task store.Task) error {
		return store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
			task.ID, task.MissionID, task.Title, task.SpecArtifact = store.NewTaskID(), "M-1", "x", w.spec
			if task.PlanNode == "" {
				task.OffPlanReason = "x"
			}
			_, err := store.ProposeTask(ctx, tx, actor, task)
			return err
		})
	}
	gate := store.Task{Role: "verify", Class: store.GateClass, Effect: "none", Repo: "bifrost-platform", BaseCommit: commitA}
	ok(t, try("controller", gate))
	for name, c := range map[string]struct {
		actor string
		task  store.Task
	}{
		"an ordinary verify":  {"controller", store.Task{Role: "verify", Class: "go", Effect: "none"}},
		"a review":            {"controller", store.Task{Role: "review", Class: store.GateClass, Effect: "none"}},
		"a gate with effect":  {"controller", store.Task{Role: "verify", Class: store.GateClass, Effect: "branch"}},
		"a gate on the plan":  {"controller", store.Task{Role: "verify", Class: store.GateClass, Effect: "none", PlanNode: s1}},
		"a gate by the Owner": {"owner", gate},
	} {
		if err := try(c.actor, c.task); err == nil || !strings.Contains(err.Error(), "23514") {
			t.Errorf("%s: %v; want the CHECK to refuse it", name, err)
		}
	}
}
