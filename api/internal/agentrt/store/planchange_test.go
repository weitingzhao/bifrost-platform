package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []store.Card
	answers map[string]store.Question
}

func (a *asker) ask(_ context.Context, c store.Card) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, prev := range a.cards {
		if prev.Key == c.Key {
			return "q_" + string(rune('a'+i)), nil
		}
	}
	a.cards = append(a.cards, c)
	return "q_" + string(rune('a'+len(a.cards)-1)), nil
}

func (a *asker) answer(id, status, option string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.answers == nil {
		a.answers = map[string]store.Question{}
	}
	a.answers[id] = store.Question{ID: id, Status: status, Option: option, AnsweredBy: "owner", AnsweredAt: "2026-10-14T20:00:00Z"}
}

func (a *asker) question(_ context.Context, id string) (store.Question, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, ok := a.answers[id]
	if !ok {
		return store.Question{ID: id, Status: "open"}, true
	}
	return q, true
}

func (w *world) activate(t *testing.T, step string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := store.SetStepState(ctx, tx, "controller", root, step, "active", "test")
		return err
	}))
}

// Within the standing authorization a change is decided and applied at
// once: a step added to an item in progress (numbered one past its largest
// sibling), the title of a step not started.
func TestPlanChangeStanding(t *testing.T) {
	w := newKernelWorld(t)
	w.activate(t, s1)
	a := &asker{}
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "one more step", []store.PlanChange{
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s4", MissionID: "M-1"},
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s5"},
		{Op: "retitle", Node: s2, Title: "s2, renamed"},
	}, a.ask)
	ok(t, err)
	nodes, _ := out["nodes"].([]any)
	if out["decision"] != "applied" || len(nodes) != 2 || nodes[0] != "R2:2.1-4" || nodes[1] != "R2:2.1-5" || len(a.cards) != 0 {
		t.Fatalf("answer %v, %d cards", out, len(a.cards))
	}
	var ordinal int
	var state, mission string
	ok(t, w.rt.QueryRow(ctx, `SELECT ordinal, state, COALESCE(mission_id, '') FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1-4'`).Scan(&ordinal, &state, &mission))
	var title string
	ok(t, w.rt.QueryRow(ctx, `SELECT title FROM agentrt.plan_node WHERE plan_node_id = $1`, s2).Scan(&title))
	if ordinal != 3 || state != "planned" || mission != "M-1" || title != "s2, renamed" {
		t.Fatalf("rows: ordinal %d state %s mission %s title %s", ordinal, state, mission, title)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event c JOIN agentrt.event d ON (c.body_json->>'decided_event_id')::bigint = d.event_id
	  WHERE c.event_type = 'plan_changed' AND d.event_type = 'plan_change_decided' AND d.body_json->>'by' = 'standing authorization'
	    AND d.body_json->>'proposal_event_id' = $1`, fmt.Sprint(out["proposal_event_id"])); n != 1 {
		t.Fatal("the change does not name its standing decision")
	}
	again, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "one more step", []store.PlanChange{
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s4", MissionID: "M-1"},
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s5"},
		{Op: "retitle", Node: s2, Title: "s2, renamed"},
	}, a.ask)
	ok(t, err)
	same(t, again, out)
	w.chainHolds(t, store.PlanStream(root))
}

// Outside it the change is tried and rolled back, then put to the Owner;
// nothing changes until he answers. A change the database would refuse is
// refused at once, with its reason, and nobody is asked (T32 through the
// plan route).
func TestPlanChangeOutside(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	before := w.footprint(t)
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "a new item", []store.PlanChange{
		{Op: "add_node", Parent: root, Kind: "item", Title: "2.2"},
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s4"},
	}, a.ask)
	ok(t, err)
	if out["decision"] != "" || out["question_id"] != "q_a" || len(a.cards) != 1 {
		t.Fatalf("answer %v, cards %v", out, a.cards)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id IN ('R2:2.2', 'R2:2.1-4')`); n != 0 {
		t.Fatalf("%d nodes before the Owner answered", n)
	}
	if after := w.footprint(t); after == before {
		t.Fatal("the proposal was not recorded")
	}
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	setRule(t, w, s1, `{"task_accepted":"`+t1+`"}`)
	before = w.footprint(t)
	for name, c := range map[string]struct {
		change store.PlanChange
		has    string
	}{
		"a step under a round":             {store.PlanChange{Op: "add_node", Parent: root, Kind: "step", StepKind: "agent", Title: "x"}, "cannot sit under"},
		"a rule naming another step's":     {store.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"task_accepted":"` + t1 + `"}`)}, "which is not on this step"},
		"a rule naming an unbound record":  {store.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"approval_executed":77}`)}, "not bound to this step"},
		"a task out of the step naming it": {store.PlanChange{Op: "move_task", Task: t1, Node: s3}, "of R2:2.1-1 names task"},
		"a node there is not":              {store.PlanChange{Op: "retitle", Node: "R2:9", Title: "x"}, "there is no"},
		"a rule that does not parse":       {store.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"nonsense":1}`)}, "does not parse"},
		"an op there is not":               {store.PlanChange{Op: "delete", Node: s3}, "one of"},
	} {
		_, perr := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "r-"+name, root, "bad", []store.PlanChange{c.change}, a.ask)
		if perr == nil || !containsFold(perr.Error(), c.has) {
			t.Errorf("%s: err = %v", name, perr)
		}
	}
	if after := w.footprint(t); after != before || len(a.cards) != 1 {
		t.Fatalf("refused changes left something (%s -> %s) or asked (%d cards)", before, after, len(a.cards))
	}
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k9", root, "x", []store.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "y"}}, nil)
	is(t, err, store.ErrAsk)
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k10", "R9", "x", []store.PlanChange{{Op: "retitle", Node: s2, Title: "y"}}, a.ask)
	is(t, err, store.ErrInvalid)
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || len(s) >= len(sub) && (indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// setRule gives a step a done rule with a plan change (the fixture of the
// controller tests).
func setRule(t *testing.T, w *world, step, rule string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller",
			Body: map[string]any{"change": "test"}}); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE agentrt.plan_node SET done_rule_json = $2::jsonb WHERE plan_node_id = $1`, step, rule)
		return err
	}))
}

// The Owner's answer decides: apply lands the change under the decision;
// the other option, a withdrawal or an expiry leave the plan as it is; a
// change that no longer fits when applied is decided failed, with the
// reason. Each proposal is decided once.
func TestPlanDecisions(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	t1, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	propose := func(key string, c store.PlanChange) string {
		t.Helper()
		out, perr := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []store.PlanChange{c}, a.ask)
		ok(t, perr)
		return out["question_id"].(string)
	}
	applied := propose("k1", store.PlanChange{Op: "add_node", Parent: root, Kind: "item", Title: "2.2"})
	refused := propose("k2", store.PlanChange{Op: "add_node", Parent: root, Kind: "item", Title: "2.3"})
	expired := propose("k3", store.PlanChange{Op: "retitle", Node: item, Title: "renamed"})
	failed := propose("k4", store.PlanChange{Op: "move_task", Task: t1, Node: s3})
	open := propose("k5", store.PlanChange{Op: "retitle", Node: root, Title: "round two"})
	// Before the answer, the step the task leaves comes to name it: moving
	// it no longer fits.
	setRule(t, w, s1, `{"task_accepted":"`+t1+`"}`)
	a.answer(applied, store.QuestionAnswered, store.CardApply)
	a.answer(refused, store.QuestionAnswered, store.CardRefuse)
	a.answer(expired, "expired", "")
	a.answer(failed, store.QuestionAnswered, store.CardApply)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question)
	ok(t, err)
	if n != 4 {
		t.Fatalf("decided %d; want 4", n)
	}
	decisions := map[string]string{}
	rows, err := w.rt.Query(ctx, `SELECT body_json->>'question_id', body_json->>'decision', COALESCE(body_json->>'error', '')
	  FROM agentrt.event WHERE event_type = 'plan_change_decided'`)
	ok(t, err)
	for rows.Next() {
		var q, d, e string
		ok(t, rows.Scan(&q, &d, &e))
		if d == "failed" && e == "" {
			t.Error("a failed decision without its reason")
		}
		decisions[q] = d
	}
	ok(t, rows.Err())
	want := map[string]string{applied: "applied", refused: "refused", expired: "expired", failed: "failed"}
	for q, d := range want {
		if decisions[q] != d {
			t.Errorf("%s: %q; want %q", q, decisions[q], d)
		}
	}
	if _, isDecided := decisions[open]; isDecided {
		t.Error("an open question decided its change")
	}
	if w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.2'`) != 1 ||
		w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.3'`) != 0 ||
		w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = $1 AND title = 'renamed'`, item) != 0 ||
		w.count(t, `SELECT count(*) FROM agentrt.task WHERE task_id = $1 AND plan_node_id = $2`, t1, s1) != 1 {
		t.Fatal("the plan does not show the decisions")
	}
	if n, err = store.DecidePlanChanges(ctx, w.rt, a.question); err != nil || n != 0 {
		t.Fatalf("a second pass decided %d (%v)", n, err)
	}
	w.chainHolds(t, store.PlanStream(root))
}

// Proposals still waiting for an answer never keep later answered ones from
// being decided: with more waiting than a page, the last is decided in one
// pass.
func TestPlanDecisionsPage(t *testing.T) {
	w := newKernelWorld(t)
	a := &asker{}
	var last string
	for i := range 101 {
		out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), fmt.Sprint("k", i), root, "test",
			[]store.PlanChange{{Op: "retitle", Node: root, Title: fmt.Sprint("title ", i)}}, func(context.Context, store.Card) (string, error) {
				return fmt.Sprint("q_", i), nil
			})
		ok(t, err)
		last = out["question_id"].(string)
	}
	a.answer(last, store.QuestionAnswered, store.CardRefuse)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question)
	if err != nil || n != 1 {
		t.Fatalf("decided %d (%v); want the last one", n, err)
	}
}
