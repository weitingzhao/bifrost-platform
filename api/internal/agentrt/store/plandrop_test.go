package store_test

import (
	"fmt"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The two plan changes added for the plan import (CUT-plan-import-2026-10-19.md
// §10 ⑥, Owner 2026-10-19 「三件照建议」): dropping a step and setting its
// marks. Both are outside the standing authorization: the Owner decides them
// on a card.

func TestPlanChangeDropAndMarks(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := &asker{}
	propose := func(key string, c store.PlanChange) (map[string]any, error) {
		t.Helper()
		return store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []store.PlanChange{c}, a.ask)
	}
	yes, no := true, false
	out, err := propose("k1", store.PlanChange{Op: "drop_step", Node: s2})
	ok(t, err)
	if out["decision"] != "" || out["question_id"] == nil {
		t.Fatalf("a drop within the standing authorization? %v", out)
	}
	drop := out["question_id"].(string)
	out, err = propose("k2", store.PlanChange{Op: "set_marks", Node: s3, OwnerStep: &yes, EffectStep: &no})
	ok(t, err)
	marks := out["question_id"].(string)
	a.answer(drop, store.QuestionAnswered, store.CardApplyKey)
	a.answer(marks, store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)
	if n != 2 {
		t.Fatalf("decided %d; want 2", n)
	}
	var state, cause string
	var causedBy, changed int64
	ok(t, w.rt.QueryRow(ctx, `SELECT n.state, e.body_json->>'cause', e.cause_event_id,
	    (SELECT max(event_id) FROM agentrt.event WHERE event_type = 'plan_changed' AND event_id < e.event_id)
	  FROM agentrt.plan_node n JOIN agentrt.event e ON e.event_id = n.state_event_id WHERE n.plan_node_id = $1`, s2).Scan(&state, &cause, &causedBy, &changed))
	if state != "dropped" || cause != "plan_changed" || causedBy != changed {
		t.Fatalf("dropped step: %s, cause %s, caused by %d, the change %d", state, cause, causedBy, changed)
	}
	var owner, effect bool
	ok(t, w.rt.QueryRow(ctx, `SELECT owner_step, effect_step FROM agentrt.plan_node WHERE plan_node_id = $1`, s3).Scan(&owner, &effect))
	if !owner || effect {
		t.Fatalf("marks of %s: owner %v effect %v", s3, owner, effect)
	}
	// Its number stays taken: the next step is numbered past it.
	w.activate(t, s1)
	out, err = propose("k3", store.PlanChange{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "after the drop"})
	ok(t, err)
	if fmt.Sprint(out["nodes"]) != "[R2:2.1-4]" {
		t.Fatalf("added after the drop: %v", out)
	}
}

// Refused when proposed, with nothing written: a step with open work, a
// step already dropped, a node that is not a step, marks with none named.
func TestPlanChangeDropAndMarksRefusals(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	a := &asker{}
	_, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	yes := true
	for name, c := range map[string]struct {
		change store.PlanChange
		want   string
	}{
		"open work":     {store.PlanChange{Op: "drop_step", Node: s1}, "has 1 unfinished tasks"},
		"an item":       {store.PlanChange{Op: "drop_step", Node: item}, "only a step is dropped"},
		"marks on item": {store.PlanChange{Op: "set_marks", Node: item, OwnerStep: &yes}, "only a step has marks"},
		"no marks":      {store.PlanChange{Op: "set_marks", Node: s2}, "owner_step, effect_step or both"},
		"no node":       {store.PlanChange{Op: "drop_step"}, "names the step"},
	} {
		before := len(a.cards)
		_, perr := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k-"+name, root, "test", []store.PlanChange{c.change}, a.ask)
		wantErr(t, perr, c.want)
		if len(a.cards) != before {
			t.Errorf("%s: a card was asked for a refused change", name)
		}
	}
	// Dropped once, a second drop is refused when it is decided.
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "d1", root, "test", []store.PlanChange{{Op: "drop_step", Node: s2}}, a.ask)
	ok(t, err)
	a.answer(out["question_id"].(string), store.QuestionAnswered, store.CardApplyKey)
	_, err = store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	ok(t, err)
	_, err = store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "d2", root, "test", []store.PlanChange{{Op: "drop_step", Node: s2}}, a.ask)
	wantErr(t, err, "dropped already")
}
