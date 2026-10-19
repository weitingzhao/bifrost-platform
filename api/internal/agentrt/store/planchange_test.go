package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []store.Card
	answers map[string]store.Question
	acked   map[string]int
}

func (a *asker) ack(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.acked == nil {
		a.acked = map[string]int{}
	}
	a.acked[id]++
	return nil
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

func (a *asker) lastID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return "q_" + string(rune('a'+len(a.cards)-1))
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	a.answer(applied, store.QuestionAnswered, store.CardApplyKey)
	a.answer(refused, store.QuestionAnswered, store.CardRefuseKey)
	a.answer(expired, "expired", "")
	a.answer(failed, store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
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
	if n, err = store.DecidePlanChanges(ctx, w.rt, a.question, a.ack); err != nil || n != 0 {
		t.Fatalf("a second pass decided %d (%v)", n, err)
	}
	// Review 180 B3, 182 B3: every answered card decided is acknowledged,
	// once (recorded); an expired one, never answered, is not.
	for _, q := range []string{applied, refused, failed} {
		if a.acked[q] != 1 {
			t.Errorf("card %s acknowledged %d times over two passes", q, a.acked[q])
		}
	}
	if a.acked[expired] != 0 || a.acked[open] != 0 {
		t.Errorf("acknowledged cards nobody answered: %v", a.acked)
	}
	w.chainHolds(t, store.PlanStream(root))
}

// Proposals still waiting for an answer never keep later answered ones from
// being decided: with more waiting than a page, the last is decided in one
// pass.
func TestPlanDecisionsPage(t *testing.T) {
	t.Parallel()
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
	a.answer(last, store.QuestionAnswered, store.CardRefuseKey)
	n, err := store.DecidePlanChanges(ctx, w.rt, a.question, nil)
	if err != nil || n != 1 {
		t.Fatalf("decided %d (%v); want the last one", n, err)
	}
}

// Review 180 A2: every change is within the tree it names.
func TestPlanChangeStaysInItsTree(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := &asker{}
	for _, n := range []store.PlanNode{
		{ID: "R3", Kind: "round", Label: "3", Title: "round 3", Ordinal: 1, Root: "R3"},
		{ID: "R3:3.1", Parent: "R3", Kind: "item", Label: "3.1", Title: "other", Root: "R3"},
		{ID: "R3:3.1-1", Parent: "R3:3.1", Kind: "step", Label: "3.1-1", Title: "other step", MissionID: "M-1", StepKind: "agent", Root: "R3"},
	} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	other, err := w.task(t, "M-1", "R3:3.1-1", "", "implement")
	ok(t, err)
	mine, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	before := w.footprint(t)
	for name, c := range map[string]store.PlanChange{
		"retitle a step of another tree":      {Op: "retitle", Node: "R3:3.1-1", Title: "x"},
		"add under another tree":              {Op: "add_node", Parent: "R3:3.1", Kind: "step", StepKind: "agent", Title: "x"},
		"move a task of another tree here":    {Op: "move_task", Task: other, Node: s1},
		"move a task to another tree":         {Op: "move_task", Task: mine, Node: "R3:3.1-1"},
		"rebind a step of another tree":       {Op: "rebind_step", Node: "R3:3.1-1", MissionID: "M-2"},
		"set the rule of another tree's step": {Op: "set_done_rule", Node: "R3:3.1-1", Rule: json.RawMessage(`{"owner_signoff":"x"}`)},
	} {
		_, perr := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "r-"+name, root, "x", []store.PlanChange{c}, a.ask)
		if !errors.Is(perr, store.ErrInvalid) || !containsFold(perr.Error(), "in plan R3, not R2") {
			t.Errorf("%s: %v", name, perr)
		}
	}
	if after := w.footprint(t); after != before || len(a.cards) != 0 {
		t.Fatalf("changes outside the tree left something (%s -> %s) or asked (%d)", before, after, len(a.cards))
	}
}

// Review 180 A3: the standing check reads what it locks. A step being moved
// to active when a retitle is proposed: the proposal waits for it and then
// sees an active step, so the retitle goes to the Owner instead of landing.
func TestStandingReadsWhatItLocks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	a := &asker{}
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = store.SetStepState(ctx, tx, "controller", root, s2, "active", "test")
	ok(t, err)
	done := make(chan map[string]any, 1)
	fail := make(chan error, 1)
	go func() {
		out, perr := store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "rename", []store.PlanChange{{Op: "retitle", Node: s2, Title: "renamed"}}, a.ask)
		if perr != nil {
			fail <- perr
			return
		}
		done <- out
	}()
	w.waitFor(t, "", time.Now().Add(5*time.Second))
	ok(t, tx.Commit(ctx))
	select {
	case perr := <-fail:
		t.Fatal(perr)
	case out := <-done:
		if out["decision"] != "" || len(a.cards) != 1 {
			t.Fatalf("a retitle of a step that became active: %v, %d cards", out, len(a.cards))
		}
	}
}

// Review 180 A3: a decision and the done-rule loop on the same step at once
// do not deadlock: the decision takes the step before the plan's stream, as
// the loop does.
func TestDecisionAgainstRuleLoop(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	a := &asker{}
	out, err := store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "rename", []store.PlanChange{{Op: "retitle", Node: item, Title: "renamed"}}, a.ask)
	ok(t, err)
	a.answer(out["question_id"].(string), store.QuestionAnswered, store.CardApplyKey)
	// The loop: the step locked, then (after the decision starts) the stream.
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`, item)
	ok(t, err)
	decided := make(chan error, 1)
	go func() {
		_, derr := store.DecidePlanChanges(ctx, pool, a.question, nil)
		decided <- derr
	}()
	w.waitFor(t, "", time.Now().Add(5*time.Second))
	_, err = store.Append(ctx, tx, store.Event{Stream: store.PlanStream(root), Type: "plan_changed", Actor: "controller", Body: map[string]any{"change": "loop"}})
	ok(t, err)
	ok(t, tx.Commit(ctx))
	ok(t, <-decided)
	if n := w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = $1 AND title = 'renamed'`, item); n != 1 {
		t.Fatal("the decision did not land")
	}
}

// Review 180 B1: a new node's number follows the numbers its siblings'
// labels carry, not their positions, and skips ids already taken.
func TestPlanChangeNumbering(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.activate(t, s1)
	for _, n := range []store.PlanNode{
		{ID: "R2:2.1-7", Parent: item, Kind: "step", Label: "2.1-7", Title: "seventh", MissionID: "M-1", StepKind: "agent", Ordinal: 3, Root: root},
		{ID: "R2:2.1-9", Parent: item, Kind: "step", Label: "odd", Title: "an odd label", MissionID: "M-1", StepKind: "agent", Ordinal: 4, Root: root},
	} {
		ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "two more", []store.PlanChange{
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "a"},
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "b"},
	}, (&asker{}).ask)
	ok(t, err)
	if fmt.Sprint(out["nodes"]) != "[R2:2.1-8 R2:2.1-10]" {
		t.Fatalf("nodes %v; want R2:2.1-8 and, skipping the taken -9, R2:2.1-10", out["nodes"])
	}
	var o8, o10 int
	ok(t, w.rt.QueryRow(ctx, `SELECT (SELECT ordinal FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1-8'),
	  (SELECT ordinal FROM agentrt.plan_node WHERE plan_node_id = 'R2:2.1-10')`).Scan(&o8, &o10))
	if o8 != 5 || o10 != 6 {
		t.Fatalf("positions %d, %d; want 5 and 6", o8, o10)
	}
}

// Review 180 B2, 182 B2: a card the question store refuses for its form is
// refused as invalid, with nothing written (the store's own limits are
// tested on the real store: TestPlanChangeCardOnTheStore).
func TestPlanChangeCardRefused(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	before := w.footprint(t)
	_, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "x",
		[]store.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "y"}}, func(context.Context, store.Card) (string, error) {
			return "", fmt.Errorf("%w: body exceeds 3000 bytes", store.ErrInvalid)
		})
	is(t, err, store.ErrInvalid)
	if after := w.footprint(t); after != before {
		t.Fatalf("a refused card left something: %s -> %s", before, after)
	}
}

// Review 182 B3: an acknowledgement that fails is tried again next pass; a
// card the store has dropped is recorded as gone and not asked about again.
func TestPlanDecisionAcks(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := &asker{}
	var ids []string
	for i := range 2 {
		out, err := store.ProposePlanChange(ctx, w.rt, wr("thread:s1"), fmt.Sprint("k", i), root, "t",
			[]store.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: fmt.Sprint("i", i)}}, a.ask)
		ok(t, err)
		id := out["question_id"].(string)
		ids = append(ids, id)
		a.answer(id, store.QuestionAnswered, store.CardRefuseKey)
	}
	calls := map[string]int{}
	failing := true
	ack := func(_ context.Context, id string) error {
		calls[id]++
		switch {
		case id == ids[1]:
			return fmt.Errorf("%w: %s", store.ErrQuestionGone, id)
		case failing:
			return fmt.Errorf("the store is busy")
		}
		return nil
	}
	if _, err := store.DecidePlanChanges(ctx, w.rt, a.question, ack); err == nil {
		t.Fatal("a failed acknowledgement was not reported")
	}
	failing = false
	for range 2 {
		_, err := store.DecidePlanChanges(ctx, w.rt, a.question, ack)
		ok(t, err)
	}
	if calls[ids[0]] != 2 || calls[ids[1]] != 1 {
		t.Fatalf("acknowledgements %v; want the failed one twice and the gone one once", calls)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND body_json->>'text' = $1`,
		"question "+ids[1]+" is gone from the question store"); n != 1 {
		t.Fatal("the gone card is not recorded")
	}
}

// Review 182 A2: a task another tree moves between the change reading
// where it is and locking it: the change is tried again and then sees it
// in the other tree.
//
// Serial: it holds the package's test hook (SetHook).
func TestMoveSeesATaskMovedMeanwhile(t *testing.T) {
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	for _, n := range []store.PlanNode{
		{ID: "R3", Kind: "round", Label: "3", Title: "round 3", Ordinal: 1, Root: "R3"},
		{ID: "R3:3.1", Parent: "R3", Kind: "item", Label: "3.1", Title: "other", Root: "R3"},
		{ID: "R3:3.1-1", Parent: "R3:3.1", Kind: "step", Label: "3.1-1", Title: "other step", MissionID: "M-1", StepKind: "agent", Root: "R3"},
	} {
		ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error { _, err := store.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	task, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	var once sync.Once
	defer store.SetHook(func(point, _ string) {
		if point == "change read" {
			once.Do(func() {
				ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
					if _, aerr := store.Append(ctx, tx, store.Event{Stream: store.PlanStream("R3"), Type: "plan_changed", Actor: "controller",
						Body: map[string]any{"change": "test: move"}}); aerr != nil {
						return aerr
					}
					_, uerr := tx.Exec(ctx, `UPDATE agentrt.task SET plan_node_id = 'R3:3.1-1' WHERE task_id = $1`, task)
					return uerr
				}))
			})
		}
	})()
	mine := `SELECT concat_ws(' ', (SELECT last_seq FROM agentrt.event_stream WHERE event_stream_id = 'plan:R2'),
	  (SELECT count(*) FROM agentrt.idempotency_key))`
	var before, after string
	ok(t, w.rt.QueryRow(ctx, mine).Scan(&before))
	_, err = store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "move", []store.PlanChange{{Op: "move_task", Task: task, Node: s3}}, (&asker{}).ask)
	if !errors.Is(err, store.ErrInvalid) || !containsFold(err.Error(), "in plan R3, not R2") {
		t.Fatalf("a move of a task moved meanwhile: %v", err)
	}
	ok(t, w.rt.QueryRow(ctx, mine).Scan(&after))
	if after != before || w.state(t, "task", task) == "" {
		t.Fatalf("the refused move left something in R2: %s -> %s", before, after)
	}
	var step string
	ok(t, w.rt.QueryRow(ctx, `SELECT plan_node_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&step))
	if step != "R3:3.1-1" {
		t.Fatalf("the task is on %s; the other tree's move stands", step)
	}
}

// Review 182 A3: a change never waits holding tasks: a task held by another
// transaction (Dispatch, Conclude) makes it try again, and past the tries it
// is busy with nothing written; a decision meeting it waits for the next
// pass.
func TestChangeMeetsAHeldTask(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	task, err := w.task(t, "M-1", s1, "", "implement")
	ok(t, err)
	lock := func() pgx.Tx {
		tx, herr := pool.Begin(ctx)
		ok(t, herr)
		_, herr = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE`, task)
		ok(t, herr)
		return tx
	}
	hold := func() pgx.Tx {
		tx := lock()
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		return tx
	}
	a := &asker{}
	released := releaseLater(t, lock(), 100*time.Millisecond)
	_, err = store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, a.ask)
	ok(t, err)
	ok(t, released())
	// Each hold lasts until the call it is for has returned, not for a time
	// the call might outlast (review 322 A2).
	held := hold()
	before := w.footprint(t)
	is(t, whileHeld(t, "a change", func() error {
		_, err := store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k2", root, "move", []store.PlanChange{{Op: "move_task", Task: task, Node: s3}}, a.ask)
		return err
	}), store.ErrBusy)
	if after := w.footprint(t); after != before {
		t.Fatalf("a busy change left something: %s -> %s", before, after)
	}
	ok(t, held.Rollback(ctx))
	a.answer(a.lastID(), store.QuestionAnswered, store.CardApplyKey)
	held = hold()
	var n int
	if derr := whileHeld(t, "a decision", func() error {
		var err error
		n, err = store.DecidePlanChanges(ctx, pool, a.question, nil)
		return err
	}); derr != nil || n != 0 {
		t.Fatalf("a decision on a held task: %d %v; want none this pass", n, derr)
	}
	ok(t, held.Rollback(ctx))
	if n, derr := store.DecidePlanChanges(ctx, pool, a.question, nil); derr != nil || n != 1 {
		t.Fatalf("the next pass: %d %v", n, derr)
	}
}

// Review 183 A3: moving a verification also takes its subject's lock NOWAIT
// (the task trigger locks it): with the subject held, as Conclude holds it,
// the change is busy at once instead of waiting holding the verification;
// once free, the move is refused by the rules (a verification sits with its
// subject).
func TestMoveVerificationMeetsItsHeldSubject(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	pool := w.db.Pool(t, w.db.Runtime)
	var subject, check string
	ok(t, store.InTx(ctx, pool, func(tx pgx.Tx) error {
		subject, check = store.NewTaskID(), store.NewTaskID()
		if _, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: subject, MissionID: "M-1", OffPlanReason: "test", Role: "implement",
			Class: "go", Title: "s", SpecArtifact: w.spec, Effect: "branch"}); err != nil {
			return err
		}
		_, err := store.ProposeTask(ctx, tx, "controller", store.Task{ID: check, MissionID: "M-1", OffPlanReason: "test", Subject: subject,
			Role: "verify", Class: "go", Title: "v", SpecArtifact: w.spec, Effect: "none"})
		return err
	}))
	tx, err := pool.Begin(ctx)
	ok(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.task WHERE task_id = $1 FOR NO KEY UPDATE`, subject)
	ok(t, err)
	// The subject stays held until the move has returned: a move that waited
	// for the lock would never return while it is held, so returning busy is
	// the proof it did not wait, whatever the clock says (review 322 A2).
	err = whileHeld(t, "a move of a verification whose subject is held", func() error {
		_, merr := store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "move", []store.PlanChange{{Op: "move_task", Task: check, Node: s1}}, (&asker{}).ask)
		return merr
	})
	ok(t, tx.Rollback(ctx))
	if !errors.Is(err, store.ErrBusy) {
		t.Fatalf("a move of a verification whose subject is held: %v; want busy", err)
	}
	_, err = store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k2", root, "move", []store.PlanChange{{Op: "move_task", Task: check, Node: s1}}, (&asker{}).ask)
	if err == nil || errors.Is(err, store.ErrBusy) || !containsFold(err.Error(), "must sit where its subject") {
		t.Fatalf("the move once the subject is free: %v", err)
	}
}
