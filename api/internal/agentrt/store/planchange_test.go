//bifrost:owner plan

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

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// asker is a fake question store: it records the cards and answers them
// later, as the Owner would.
type asker struct {
	mu      sync.Mutex
	cards   []planstore.Card
	answers map[string]seenfacts.Question
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

func (a *asker) ask(_ context.Context, c planstore.Card) (string, error) {
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
		a.answers = map[string]seenfacts.Question{}
	}
	a.answers[id] = seenfacts.Question{ID: id, Status: status, Option: option, AnsweredBy: "owner", AnsweredAt: "2026-10-14T20:00:00Z"}
}

func (a *asker) question(_ context.Context, id string) (seenfacts.Question, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, ok := a.answers[id]
	if !ok {
		return seenfacts.Question{ID: id, Status: "open"}, true
	}
	return q, true
}

func (w *world) activate(t *testing.T, step string) {
	t.Helper()
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.SetStepState(ctx, tx, "controller", root, step, "active", "test")
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
	out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "one more step", []planstore.PlanChange{
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
	again, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "one more step", []planstore.PlanChange{
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s4", MissionID: "M-1"},
		{Op: "add_node", Parent: item, Kind: "step", StepKind: "agent", Title: "s5"},
		{Op: "retitle", Node: s2, Title: "s2, renamed"},
	}, a.ask)
	ok(t, err)
	same(t, again, out)
	w.chainHolds(t, planstore.PlanStream(root))
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
	out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "a new item", []planstore.PlanChange{
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
	// A task id no row answers to: the tasks' table is gone (0020).
	t1 := "t_01M5192P0JPMJAP63ZKRPXJ2VV"
	before = w.footprint(t)
	for name, c := range map[string]struct {
		change planstore.PlanChange
		has    string
	}{
		"a step under a round":             {planstore.PlanChange{Op: "add_node", Parent: root, Kind: "step", StepKind: "agent", Title: "x"}, "cannot sit under"},
		"a rule naming an unbound record":  {planstore.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"approval_executed":77}`)}, "not bound to this step"},
		"a task out of the step naming it": {planstore.PlanChange{Op: "move_task", Task: t1, Node: s3}, "move_task is retired"},
		"a node there is not":              {planstore.PlanChange{Op: "retitle", Node: "R2:9", Title: "x"}, "there is no"},
		"a rule that does not parse":       {planstore.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"nonsense":1}`)}, "does not parse"},
		"an op there is not":               {planstore.PlanChange{Op: "delete", Node: s3}, "one of"},
		// R5:5.5-15: a rule counting the step's own work needs the sign-off gate.
		"step work without a sign-off":  {planstore.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}}]}`)}, "R5:5.5-15"},
		"step work, sign-off in an any": {planstore.PlanChange{Op: "set_done_rule", Node: s3, Rule: json.RawMessage(`{"any":[{"step_landed":{}},{"owner_signoff":"r"}]}`)}, "owner_signoff among its parts"},
	} {
		_, perr := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "r-"+name, root, "bad", []planstore.PlanChange{c.change}, a.ask)
		if perr == nil || !containsFold(perr.Error(), c.has) {
			t.Errorf("%s: err = %v", name, perr)
		}
	}
	if after := w.footprint(t); after != before || len(a.cards) != 1 {
		t.Fatalf("refused changes left something (%s -> %s) or asked (%d cards)", before, after, len(a.cards))
	}
	// The same step work behind the sign-off gate is not refused (it is put to the Owner like any other change here).
	gated := json.RawMessage(`{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}},{"owner_signoff":"s3 accepted"}]}`)
	_, err = planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k-gated", root, "gated", []planstore.PlanChange{{Op: "set_done_rule", Node: s3, Rule: gated}}, a.ask)
	ok(t, err)
	_, err = planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k9", root, "x", []planstore.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "y"}}, nil)
	is(t, err, planstore.ErrAsk)
	_, err = planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k10", "R9", "x", []planstore.PlanChange{{Op: "retitle", Node: s2, Title: "y"}}, a.ask)
	is(t, err, eventlog.ErrInvalid)
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

// The Owner's answer decides: apply lands the change under the decision;
// the other option, a withdrawal or an expiry leave the plan as it is; a
// change that no longer fits when applied is decided failed, with the
// reason. Each proposal is decided once.
func TestPlanDecisions(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := &asker{}
	propose := func(key string, c planstore.PlanChange) string {
		t.Helper()
		out, perr := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), key, root, "test", []planstore.PlanChange{c}, a.ask)
		ok(t, perr)
		return out["question_id"].(string)
	}
	applied := propose("k1", planstore.PlanChange{Op: "add_node", Parent: root, Kind: "item", Title: "2.2"})
	refused := propose("k2", planstore.PlanChange{Op: "add_node", Parent: root, Kind: "item", Title: "2.3"})
	expired := propose("k3", planstore.PlanChange{Op: "retitle", Node: item, Title: "renamed"})
	failed := propose("k4", planstore.PlanChange{Op: "drop_step", Node: s3})
	open := propose("k5", planstore.PlanChange{Op: "retitle", Node: root, Title: "round two"})
	// Before the answer, the step is dropped by another hand: dropping it
	// no longer fits.
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, err := planstore.SetStepState(ctx, tx, "controller", root, s3, "dropped", "test")
		return err
	}))
	a.answer(applied, seenfacts.QuestionAnswered, planstore.CardApplyKey)
	a.answer(refused, seenfacts.QuestionAnswered, planstore.CardRefuseKey)
	a.answer(expired, "expired", "")
	a.answer(failed, seenfacts.QuestionAnswered, planstore.CardApplyKey)
	n, err := planstore.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
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
		w.count(t, `SELECT count(*) FROM agentrt.plan_node WHERE plan_node_id = $1 AND title = 'renamed'`, item) != 0 {
		t.Fatal("the plan does not show the decisions")
	}
	if n, err = planstore.DecidePlanChanges(ctx, w.rt, a.question, a.ack); err != nil || n != 0 {
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
	w.chainHolds(t, planstore.PlanStream(root))
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
		out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), fmt.Sprint("k", i), root, "test",
			[]planstore.PlanChange{{Op: "retitle", Node: root, Title: fmt.Sprint("title ", i)}}, func(context.Context, planstore.Card) (string, error) {
				return fmt.Sprint("q_", i), nil
			})
		ok(t, err)
		last = out["question_id"].(string)
	}
	a.answer(last, seenfacts.QuestionAnswered, planstore.CardRefuseKey)
	n, err := planstore.DecidePlanChanges(ctx, w.rt, a.question, nil)
	if err != nil || n != 1 {
		t.Fatalf("decided %d (%v); want the last one", n, err)
	}
}

// Review 180 A2: every change is within the tree it names.
func TestPlanChangeStaysInItsTree(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := &asker{}
	for _, n := range []planstore.PlanNode{
		{ID: "R3", Kind: "round", Label: "3", Title: "round 3", Ordinal: 1, Root: "R3"},
		{ID: "R3:3.1", Parent: "R3", Kind: "item", Label: "3.1", Title: "other", Root: "R3"},
		{ID: "R3:3.1-1", Parent: "R3:3.1", Kind: "step", Label: "3.1-1", Title: "other step", MissionID: "M-1", StepKind: "agent", Root: "R3"},
	} {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := planstore.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	before := w.footprint(t)
	for name, c := range map[string]planstore.PlanChange{
		"retitle a step of another tree":      {Op: "retitle", Node: "R3:3.1-1", Title: "x"},
		"add under another tree":              {Op: "add_node", Parent: "R3:3.1", Kind: "step", StepKind: "agent", Title: "x"},
		"rebind a step of another tree":       {Op: "rebind_step", Node: "R3:3.1-1", MissionID: "M-2"},
		"set the rule of another tree's step": {Op: "set_done_rule", Node: "R3:3.1-1", Rule: json.RawMessage(`{"owner_signoff":"x"}`)},
	} {
		_, perr := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "r-"+name, root, "x", []planstore.PlanChange{c}, a.ask)
		if !errors.Is(perr, eventlog.ErrInvalid) || !containsFold(perr.Error(), "in plan R3, not R2") {
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
	_, err = planstore.SetStepState(ctx, tx, "controller", root, s2, "active", "test")
	ok(t, err)
	done := make(chan map[string]any, 1)
	fail := make(chan error, 1)
	go func() {
		out, perr := planstore.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "rename", []planstore.PlanChange{{Op: "retitle", Node: s2, Title: "renamed"}}, a.ask)
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
	out, err := planstore.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "rename", []planstore.PlanChange{{Op: "retitle", Node: item, Title: "renamed"}}, a.ask)
	ok(t, err)
	a.answer(out["question_id"].(string), seenfacts.QuestionAnswered, planstore.CardApplyKey)
	// The loop: the step locked, then (after the decision starts) the stream.
	tx, err := pool.Begin(ctx)
	ok(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`, item)
	ok(t, err)
	decided := make(chan error, 1)
	go func() {
		_, derr := planstore.DecidePlanChanges(ctx, pool, a.question, nil)
		decided <- derr
	}()
	w.waitFor(t, "", time.Now().Add(5*time.Second))
	_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream(root), Type: "plan_changed", Actor: "controller", Body: map[string]any{"change": "loop"}})
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
	for _, n := range []planstore.PlanNode{
		{ID: "R2:2.1-7", Parent: item, Kind: "step", Label: "2.1-7", Title: "seventh", MissionID: "M-1", StepKind: "agent", Ordinal: 3, Root: root},
		{ID: "R2:2.1-9", Parent: item, Kind: "step", Label: "odd", Title: "an odd label", MissionID: "M-1", StepKind: "agent", Ordinal: 4, Root: root},
	} {
		ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error { _, err := planstore.CreatePlanNode(ctx, tx, "controller", n); return err }))
	}
	out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "two more", []planstore.PlanChange{
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
	_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k1", root, "x",
		[]planstore.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: "y"}}, func(context.Context, planstore.Card) (string, error) {
			return "", fmt.Errorf("%w: body exceeds 3000 bytes", eventlog.ErrInvalid)
		})
	is(t, err, eventlog.ErrInvalid)
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
		out, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), fmt.Sprint("k", i), root, "t",
			[]planstore.PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: fmt.Sprint("i", i)}}, a.ask)
		ok(t, err)
		id := out["question_id"].(string)
		ids = append(ids, id)
		a.answer(id, seenfacts.QuestionAnswered, planstore.CardRefuseKey)
	}
	calls := map[string]int{}
	failing := true
	ack := func(_ context.Context, id string) error {
		calls[id]++
		switch {
		case id == ids[1]:
			return fmt.Errorf("%w: %s", planstore.ErrQuestionGone, id)
		case failing:
			return fmt.Errorf("the store is busy")
		}
		return nil
	}
	if _, err := planstore.DecidePlanChanges(ctx, w.rt, a.question, ack); err == nil {
		t.Fatal("a failed acknowledgement was not reported")
	}
	failing = false
	for range 2 {
		_, err := planstore.DecidePlanChanges(ctx, w.rt, a.question, ack)
		ok(t, err)
	}
	if calls[ids[0]] != 2 || calls[ids[1]] != 1 {
		t.Fatalf("acknowledgements %v; want the failed one twice and the gone one once", calls)
	}
	if n := w.count(t, `SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_card_settled' AND body_json->>'text' = $1`,
		"question "+ids[1]+" is gone from the question store"); n != 1 {
		t.Fatal("the gone card is not recorded")
	}
}

// R5:5.5-15 review round 1: a proposal queued before the sign-off gate
// shipped, approved after it, is refused where the rule is written; the
// approval of a rule change is not the Owner's sign-off on the step.
func TestPlanChangeQueuedUngatedRuleIsRefusedOnApply(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	a := &asker{}
	// A proposal made the usual way, so the writer and its card exist.
	_, err := planstore.ProposePlanChange(ctx, w.rt, wr("thread:s1"), "k-item", root, "a new item", []planstore.PlanChange{
		{Op: "add_node", Parent: root, Kind: "item", Title: "2.2"}}, a.ask)
	ok(t, err)
	q, err := a.ask(ctx, planstore.Card{})
	ok(t, err)
	var ungated map[string]any
	ok(t, json.Unmarshal([]byte(`{"all":[{"step_tasks":{"state":"accepted"}},{"step_landed":{}},{"step_stg_settled":{}}]}`), &ungated))
	ok(t, eventlog.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		_, aerr := eventlog.Append(ctx, tx, eventlog.Event{Stream: planstore.PlanStream(root), Type: "plan_change_proposed", Actor: "thread:s1",
			Body: map[string]any{"changes": []any{map[string]any{"op": "set_done_rule", "node": s3, "rule": ungated}},
				"reason": "queued before the sign-off gate", "standing": false, "token": "operator", "question_id": q}})
		return aerr
	}))
	a.answer(q, seenfacts.QuestionAnswered, planstore.CardApplyKey)
	_, err = planstore.DecidePlanChanges(ctx, w.rt, a.question, a.ack)
	ok(t, err)
	var rule string
	ok(t, w.rt.QueryRow(ctx, `SELECT done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = $1`, s3).Scan(&rule))
	if rule != "{}" {
		t.Fatalf("the queued ungated rule was written: %s", rule)
	}
	var decision, why string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'decision', COALESCE(body_json->>'error', '') FROM agentrt.event
	  WHERE event_type = 'plan_change_decided' AND body_json->>'question_id' = $1`, q).Scan(&decision, &why))
	if decision != "failed" || !containsFold(why, "R5:5.5-15") {
		t.Fatalf("decision %q (%s), want failed naming R5:5.5-15", decision, why)
	}
}
