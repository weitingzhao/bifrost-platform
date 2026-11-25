package planstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
)

// Proposing and deciding plan changes, through the contract (doc.go).

func addStep(parent, title string) planstore.PlanChange {
	return planstore.PlanChange{Op: "add_node", Parent: parent, Kind: "step", StepKind: "agent", Title: title}
}

func retitle(node, title string) planstore.PlanChange {
	return planstore.PlanChange{Op: "retitle", Node: node, Title: title}
}

func doneRule(node, rule string) planstore.PlanChange {
	return planstore.PlanChange{Op: "set_done_rule", Node: node, Rule: json.RawMessage(rule)}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanChangeStanding: a change inside the standing authorization must land at once under its own decision and a retry must not land it twice; the wrong outcome is a second step, or a change with no decision naming it.
func TestContractStandingChangeLandsAtOnce(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	changes := []planstore.PlanChange{addStep(item1, "added"), addStep(item1, "added too"), retitle(stepA, "renamed")}
	out, err := w.propose("standing", changes...)
	must(t, err)
	if out["decision"] != "applied" || fmt.Sprint(out["nodes"]) != "[R2:2.1-5 R2:2.1-6]" || w.desk.count() != 1 {
		t.Fatalf("answer %v, %d cards (the import's is the 1)", out, w.desk.count())
	}
	if got := w.node("R2:2.1-5"); got.kind != "step" || got.state != "planned" || got.title != "added" {
		t.Fatalf("the added step: %+v", got)
	}
	if got := w.node(stepA).title; got != "renamed" {
		t.Fatalf("title %q", got)
	}
	if n := w.count(`SELECT count(*) FROM agentrt.event c JOIN agentrt.event d ON (c.body_json->>'decided_event_id')::bigint = d.event_id
	  WHERE c.event_type = 'plan_changed' AND c.event_stream_id = 'plan:R2' AND d.event_type = 'plan_change_decided'
	    AND d.actor_id = 'controller' AND d.body_json->>'by' = 'standing authorization' AND d.body_json->>'proposal_event_id' = $1`,
		fmt.Sprint(out["proposal_event_id"])); n != 1 {
		t.Fatalf("%d plan_changed events name the standing decision; want 1", n)
	}
	before := w.footprint()
	again, err := w.propose("standing", changes...)
	must(t, err)
	if fmt.Sprint(again) != fmt.Sprint(out) || w.footprint() != before {
		t.Fatalf("the retry answered %v (first %v) or wrote something", again, out)
	}
	// The same key with another request is refused and changes nothing.
	_, err = w.propose("standing", retitle(stepA, "something else"))
	wantIs(t, "the key reused", err, actor.ErrConflict)
	// A thread's session is its token's.
	_, err = planstore.ProposePlanChange(contractCtx, w.rt, actor.Writer{Actor: writer.Actor, Token: "another"}, "other-token", planRoot, "x",
		[]planstore.PlanChange{retitle(stepA, "taken over")}, w.desk.ask)
	wantIs(t, "another token", err, actor.ErrNotYours)
	if w.footprint() != before {
		t.Fatal("a refused proposal left something")
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanChangeOutside and the card half of TestPlanDecisions: what the standing authorization does not cover must reach the Owner as a card and change nothing until he answers; the wrong outcome is a plan edited without him.
func TestContractChangeOutsideWaitsForTheOwner(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	for name, c := range map[string]planstore.PlanChange{
		"a step under an item with nothing active": addStep(item2, "x"),
		"an item":                  {Op: "add_node", Parent: planRoot, Kind: "item", Title: "2.3"},
		"the title of a live step": retitle(stepLive, "x"),
		"the title of an item":     retitle(item1, "x"),
		"a done rule":              doneRule(stepA, `{"owner_signoff":"x"}`),
		// 2026-11-25: the signed plan-standing grant does not name the reading atom.
		"a reading rule": doneRule(stepA, `{"reading":{"source":"catalog.status","path":"pool.oldest_days","op":"<=","value":14}}`),
		"a drop":         {Op: "drop_step", Node: stepB},
	} {
		nodes := w.count(`SELECT count(*) FROM agentrt.plan_node`)
		title := w.node(item1).title
		cards := w.desk.count()
		out, err := w.propose("outside-"+name, c)
		must(t, err)
		if out["decision"] != "" || out["question_id"] == nil || out["waiting"] == nil || w.desk.count() != cards+1 {
			t.Fatalf("%s: answer %v, %d cards", name, out, w.desk.count())
		}
		if w.count(`SELECT count(*) FROM agentrt.plan_node`) != nodes || w.node(item1).title != title || w.decisionOf(out) != "" {
			t.Fatalf("%s: the plan moved before the Owner answered", name)
		}
	}
	if got := w.node(stepB).state; got != "planned" {
		t.Fatalf("a dropped step before the answer: %s", got)
	}
	if n := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_change_proposed' AND event_stream_id = 'plan:R2'
	  AND body_json->>'standing' = 'false' AND body_json ? 'question_id' AND actor_id = $1 AND body_json->'changes'->0->>'op' <> 'import'`, writer.Actor); n != 7 {
		t.Fatalf("%d proposals recorded with their card; want 7", n)
	}
}

// Origin: 2026-11-18, TD-582: the card a proposal gets is the wire to the question store (server/questions_wire.go adds CardApply and CardRefuse to a card with no options); a changed key would make a retry ask a second card, and a card asked ByRule with no rule in force would bypass the Owner.
func TestContractPlanChangeCard(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	if planstore.CardApply != "Apply the change" || planstore.CardRefuse != "Do not apply it" {
		t.Fatalf("the options read %q and %q", planstore.CardApply, planstore.CardRefuse)
	}
	failed := errors.New("the store is down")
	var keys []string
	refuse := func(_ context.Context, c planstore.Card) (string, error) {
		keys = append(keys, c.Key)
		return "", failed
	}
	change := []planstore.PlanChange{retitle(item1, "renamed")}
	reason := "first line\nand the rest"
	_, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "card", planRoot, reason, change, refuse)
	wantIs(t, "the first try", err, planstore.ErrAsk)
	out, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "card", planRoot, reason, change, w.desk.ask)
	must(t, err)
	c := w.desk.last()
	if len(keys) != 1 || c.Key != keys[0] || c.Key == "" {
		t.Fatalf("the retry's card key %q; the first try's %v", c.Key, keys)
	}
	if c.Title != "Plan change to R2: first line" || !containsAll(c.Body, "R2", "first line") || len(c.Options) != 0 || c.ByRule {
		t.Fatalf("card %+v", c)
	}
	if w.questionOf(out) == "" {
		t.Fatal("no question recorded")
	}
	// Another key is another card, the same content or not.
	_, err = planstore.ProposePlanChange(contractCtx, w.rt, writer, "card-2", planRoot, reason, change, w.desk.ask)
	must(t, err)
	if w.desk.last().Key == c.Key {
		t.Fatal("two proposals share a card key")
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanDecisions and TestPlanChangeDropAndMarks: only the first option applies, each proposal is decided once, a change that no longer fits is decided failed with its reason, and each answered card is acknowledged once; the wrong outcomes are a refused change landing or a card counted against the quota for a week.
func TestContractDecisions(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	yes, no := true, false
	propose := func(key string, c planstore.PlanChange) map[string]any {
		t.Helper()
		out, err := w.propose(key, c)
		must(t, err)
		return out
	}
	applied := propose("applied", planstore.PlanChange{Op: "add_node", Parent: planRoot, Kind: "item", Title: "added item"})
	marks := propose("marks", planstore.PlanChange{Op: "set_marks", Node: stepA, OwnerStep: &yes, EffectStep: &no})
	refused := propose("refused", retitle(item1, "refused title"))
	expired := propose("expired", retitle(item1, "expired title"))
	withdrawn := propose("withdrawn", retitle(item1, "withdrawn title"))
	open := propose("open", retitle(item1, "open title"))
	dropped := propose("dropped", planstore.PlanChange{Op: "drop_step", Node: stepB})
	late := propose("late", planstore.PlanChange{Op: "drop_step", Node: stepB})
	for _, p := range []map[string]any{applied, marks, dropped, late} {
		w.desk.answer(w.questionOf(p), optionApply)
	}
	w.desk.answer(w.questionOf(refused), optionRefuse)
	w.desk.close(w.questionOf(expired), "expired")
	w.desk.close(w.questionOf(withdrawn), "withdrawn")
	if n := w.decide(); n != 7 {
		t.Fatalf("decided %d; want 7", n)
	}
	for name, c := range map[string]struct {
		out  map[string]any
		want string
	}{"applied": {applied, "applied"}, "marks": {marks, "applied"}, "refused": {refused, "refused"}, "expired": {expired, "expired"},
		"withdrawn": {withdrawn, "withdrawn"}, "open": {open, ""}, "dropped": {dropped, "applied"}, "late": {late, "failed"}} {
		if got := w.decisionOf(c.out); got != c.want {
			t.Errorf("%s: decided %q; want %q", name, got, c.want)
		}
	}
	if why := w.decisionError(late); !strings.Contains(why, "dropped already") {
		t.Errorf("the failed decision says %q", why)
	}
	if !w.hasNode("R2:2.3") {
		t.Error("the applied item is not in the plan")
	}
	if got := w.node(item1).title; got != "数据层" {
		t.Errorf("the item's title is %q: an unapplied change landed", got)
	}
	if got := w.node(stepB).state; got != "dropped" {
		t.Errorf("the dropped step is %s", got)
	}
	var owner, effect bool
	must(t, w.rt.QueryRow(contractCtx, `SELECT owner_step, effect_step FROM agentrt.plan_node WHERE plan_node_id = $1`, stepA).Scan(&owner, &effect))
	if !owner || effect {
		t.Errorf("marks: owner %v, effect %v", owner, effect)
	}
	// A dropped step keeps its number: the next one is numbered past it.
	out, err := w.propose("after-drop", addStep(item1, "after"))
	must(t, err)
	if fmt.Sprint(out["nodes"]) != "[R2:2.1-5]" {
		t.Errorf("added after the drop: %v", out["nodes"])
	}
	// A second pass decides nothing and acknowledges nothing again.
	if n := w.decide(); n != 0 {
		t.Fatalf("the second pass decided %d", n)
	}
	for _, p := range []map[string]any{applied, marks, refused, dropped, late} {
		if got := w.desk.ackCount(w.questionOf(p)); got != 1 {
			t.Errorf("card %v acknowledged %d times", p["question_id"], got)
		}
	}
	for _, p := range []map[string]any{expired, withdrawn, open} {
		if got := w.desk.ackCount(w.questionOf(p)); got != 0 {
			t.Errorf("unanswered card %v acknowledged %d times", p["question_id"], got)
		}
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanChangeOutside (its refusal table), TestPlanChangeStaysInItsTree and TestPlanChangeDropAndMarksRefusals: a change that cannot land is refused when proposed, with nothing written and nobody asked; the wrong outcome is a card for a change that can never apply, or a change reaching into another plan.
func TestContractIllegalChangesWriteNothing(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	yes := true
	zero := 0
	for name, c := range map[string]struct {
		change  planstore.PlanChange
		has     string
		invalid bool
	}{
		"an op there is not":       {planstore.PlanChange{Op: "delete", Node: stepA}, "is one of", true},
		"move_task is retired":     {planstore.PlanChange{Op: "move_task", Task: "t", Node: stepA}, "retired", true},
		"a node there is not":      {retitle("R2:9", "x"), "there is no node", true},
		"a node of another plan":   {retitle(archRowA, "x"), "in plan ARCH, not R2", true},
		"a parent of another plan": {addStep(archRoot, "x"), "in plan ARCH, not R2", true},
		"a step without a kind":    {planstore.PlanChange{Op: "add_node", Parent: item1, Kind: "step", Title: "x"}, "step kinds", true},
		"an item with a mission":   {planstore.PlanChange{Op: "add_node", Parent: planRoot, Kind: "item", Title: "x", MissionID: "M"}, "no step kind or mission", true},
		"a retitle with no title":  {retitle(stepA, ""), "new title", true},
		"a rule that won't parse":  {doneRule(stepA, `{"nonsense":1}`), "does not parse", true},
		"step work, no sign-off":   {doneRule(stepA, `{"step_landed":{}}`), "owner_signoff", true},
		"step work under an any":   {doneRule(stepA, `{"any":[{"step_landed":{}},{"owner_signoff":"r"}]}`), "owner_signoff among its parts", true},
		"a drop naming nothing":    {planstore.PlanChange{Op: "drop_step"}, "names the step", true},
		"marks on an item":         {planstore.PlanChange{Op: "set_marks", Node: item1, OwnerStep: &yes}, "only a step has marks", true},
		"no marks named":           {planstore.PlanChange{Op: "set_marks", Node: stepA}, "owner_step, effect_step or both", true},
		"dropping an item":         {planstore.PlanChange{Op: "drop_step", Node: item1}, "only a step is dropped", true},
		"an artifact on a retitle": {planstore.PlanChange{Op: "retitle", Node: stepA, Title: "x", Artifact: "sha256:x"}, "adopt_plan_view", true},
		"an adoption, no artifact": {planstore.PlanChange{Op: "adopt_plan_view", ExpectedVersion: &zero}, "expected_version", true},
		// The database's own refusals, tried before the Owner is asked.
		"a step under a round":   {addStep(planRoot, "x"), "cannot sit under", false},
		"a rule of an unbound q": {doneRule(stepA, `{"question_answered":{"question":"Q","option":"A"}}`), "not bound to this step", false},
	} {
		before, cards := w.footprint(), w.desk.count()
		_, err := w.propose("illegal-"+name, c.change)
		if err == nil || !strings.Contains(err.Error(), c.has) {
			t.Errorf("%s: err = %v; want it to say %q", name, err, c.has)
			continue
		}
		if c.invalid && !errors.Is(err, eventlog.ErrInvalid) {
			t.Errorf("%s: %v is not ErrInvalid", name, err)
		}
		if w.footprint() != before || w.desk.count() != cards {
			t.Errorf("%s: the refusal left something, or asked", name)
		}
	}
	// The proposal itself.
	one := []planstore.PlanChange{retitle(stepA, "x")}
	for name, call := range map[string]func() (map[string]any, error){
		"no key": func() (map[string]any, error) {
			return planstore.ProposePlanChange(contractCtx, w.rt, writer, "", planRoot, "r", one, w.desk.ask)
		},
		"no root": func() (map[string]any, error) {
			return planstore.ProposePlanChange(contractCtx, w.rt, writer, "k", "", "r", one, w.desk.ask)
		},
		"no reason": func() (map[string]any, error) {
			return planstore.ProposePlanChange(contractCtx, w.rt, writer, "k", planRoot, "", one, w.desk.ask)
		},
		"no changes": func() (map[string]any, error) {
			return planstore.ProposePlanChange(contractCtx, w.rt, writer, "k", planRoot, "r", nil, w.desk.ask)
		},
		"not a root": func() (map[string]any, error) {
			return planstore.ProposePlanChange(contractCtx, w.rt, writer, "k2", item1, "r", []planstore.PlanChange{addStep("R2:2.1", "x")}, w.desk.ask)
		},
		"no such one": func() (map[string]any, error) {
			return planstore.ProposePlanChange(contractCtx, w.rt, writer, "k3", "R9", "r", one, w.desk.ask)
		},
	} {
		before := w.footprint()
		_, err := call()
		wantIs(t, name, err, eventlog.ErrInvalid)
		if w.footprint() != before {
			t.Errorf("%s: the refusal left something", name)
		}
	}
	// The positive control of the table's gate: the same rule behind the
	// sign-off is proposed, not refused.
	if _, err := w.propose("gated", doneRule(stepA, `{"all":[{"step_landed":{}},{"owner_signoff":"r"}]}`)); err != nil {
		t.Fatalf("a gated rule was refused: %v", err)
	}
}

// Origin: 2026-11-18, TD-582: ErrAsk is what the route turns into "try later", and the contract says nothing was written; the contract version of store's TestPlanChangeCardRefused and the ask cases of TestPlanChangeOutside. The wrong outcomes are a proposal recorded with no card, or a card the question store refused for its form reported as a passing failure.
func TestContractAskFailure(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	change := []planstore.PlanChange{retitle(item1, "x")}
	before, cards := w.footprint(), w.desk.count()
	_, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "ask", planRoot, "r", change, nil)
	wantIs(t, "no question store", err, planstore.ErrAsk)
	_, err = planstore.ProposePlanChange(contractCtx, w.rt, writer, "ask", planRoot, "r", change,
		func(context.Context, planstore.Card) (string, error) { return "", errors.New("full") })
	wantIs(t, "the store refuses", err, planstore.ErrAsk)
	_, err = planstore.ProposePlanChange(contractCtx, w.rt, writer, "ask", planRoot, "r", change,
		func(context.Context, planstore.Card) (string, error) {
			return "", eventlog.Invalid("the body is too long")
		})
	wantIs(t, "a card of the wrong form", err, eventlog.ErrInvalid)
	if errors.Is(err, planstore.ErrAsk) {
		t.Fatalf("a card of the wrong form is reported as ErrAsk: %v", err)
	}
	if w.footprint() != before || w.desk.count() != cards {
		t.Fatal("a failed ask left something")
	}
	// The key was not used up: the same proposal goes through.
	out, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "ask", planRoot, "r", change, w.desk.ask)
	must(t, err)
	w.questionOf(out)
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanDecisionAcks and TestImportDecisionAcknowledged: a failed acknowledgement is reported and tried again, and ErrQuestionGone closes the card for good; the wrong outcome is asking the question store about a dropped card every pass, for ever.
func TestContractAcknowledgements(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	var ids []string
	for _, key := range []string{"kept", "gone"} {
		out, err := w.propose(key, retitle(item1, key))
		must(t, err)
		id := w.questionOf(out)
		ids = append(ids, id)
		w.desk.answer(id, optionRefuse)
	}
	calls := map[string]int{}
	failing := true
	ack := func(_ context.Context, id string) error {
		calls[id]++
		switch {
		case id == ids[1]:
			return fmt.Errorf("%w: %s", planstore.ErrQuestionGone, id)
		case failing:
			return errors.New("the store is busy")
		}
		return nil
	}
	n, err := planstore.DecidePlanChanges(contractCtx, w.rt, w.desk.question, ack)
	if n != 2 || err == nil {
		t.Fatalf("a failed acknowledgement: %d decided, err %v", n, err)
	}
	failing = false
	for range 2 {
		_, err = planstore.DecidePlanChanges(contractCtx, w.rt, w.desk.question, ack)
		must(t, err)
	}
	if calls[ids[0]] != 2 || calls[ids[1]] != 1 {
		t.Fatalf("acknowledgements %v; want the failed one twice, the gone one once", calls)
	}
	for text, want := range map[string]int{
		"question " + ids[1] + " is gone from the question store": 1,
		"acknowledged question " + ids[0]:                         1,
	} {
		if got := w.count(`SELECT count(*) FROM agentrt.event WHERE event_type = 'plan_card_settled' AND event_stream_id = 'plan:R2'
		  AND body_json->>'text' = $1`, text); got != want {
			t.Errorf("%q recorded %d times", text, got)
		}
	}
}

// Origin: 2026-11-18, TD-582: no test held ErrBusy before (it was only mapped by the route): a proposal that keeps meeting a held row must give up with ErrBusy and leave its key free, not hang or report a raw database error; the positive control is the same proposal going through once the row is free.
func TestContractBusyPlan(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	conn := w.db.Connect(t, w.db.Runtime)
	_, err := conn.Exec(contractCtx, `SET lock_timeout = '20ms'`)
	must(t, err)
	holder, err := w.rt.Begin(contractCtx)
	must(t, err)
	defer func() { _ = holder.Rollback(contractCtx) }()
	_, err = holder.Exec(contractCtx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`, stepA)
	must(t, err)
	change := []planstore.PlanChange{retitle(stepA, "renamed")}
	before := w.footprint()
	_, err = planstore.ProposePlanChange(contractCtx, conn, writer, "busy", planRoot, "r", change, w.desk.ask)
	wantIs(t, "a held row", err, planstore.ErrBusy)
	if w.footprint() != before {
		t.Fatal("a busy proposal left something")
	}
	must(t, holder.Rollback(contractCtx))
	out, err := planstore.ProposePlanChange(contractCtx, conn, writer, "busy", planRoot, "r", change, w.desk.ask)
	must(t, err)
	if out["decision"] != "applied" {
		t.Fatalf("after the row was freed: %v", out)
	}
}

// waitForAdvisoryWaiter waits until some session of this database waits for
// an advisory lock; a case that never gets there proves nothing.
func (w *planWorld) waitForAdvisoryWaiter() {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for w.count(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'`) == 0 {
		if time.Now().After(deadline) {
			w.t.Fatal("nothing waits for the tree's lock; the case proves nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Origin: 2026-11-18, TD-582: externalwork takes LockTree through its port to serialize with plan changes, and the contract says the lock is per tree: without it a change and a registration can both read a tree as it was; with a lock wider than the tree, one plan stalls every other. The contract side of store's TestStandingReadsWhatItLocks, without its internal SetStepState.
func TestContractLockTreeHoldsItsTreeOnly(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	w.importAndApply("import-arch", archRequest(t, ""))
	tx, err := w.rt.Begin(contractCtx)
	must(t, err)
	defer func() { _ = tx.Rollback(contractCtx) }()
	must(t, planstore.LockTree(contractCtx, tx, planRoot))
	// Another tree is not held.
	other, err := planstore.ProposePlanChange(contractCtx, w.rt, writer, "other-tree", archRoot, "r", []planstore.PlanChange{retitle(archRowA, "x")}, w.desk.ask)
	must(t, err)
	w.questionOf(other)
	type result struct {
		out map[string]any
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, perr := w.propose("held", retitle(stepA, "renamed"))
		done <- result{out, perr}
	}()
	w.waitForAdvisoryWaiter()
	select {
	case r := <-done:
		t.Fatalf("a proposal went through the tree's lock: %v %v", r.out, r.err)
	default:
	}
	must(t, tx.Commit(contractCtx))
	r := <-done
	must(t, r.err)
	if r.out["decision"] != "applied" {
		t.Fatalf("after the lock: %v", r.out)
	}
}

// Origin: 2026-11-18, TD-582: the contract version of the question atom of store's TestStepRules: a done rule binds what it names through the change's bind list, the JSON the route receives, and the step moves only on that record with that option; the wrong outcome is a step done by another answer.
func TestContractBoundRuleMovesTheStepOnItsFact(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	var c planstore.PlanChange
	must(t, json.Unmarshal([]byte(`{"op":"set_done_rule","node":"`+stepA+`","rule":{"question_answered":{"question":"Q3","option":"A"}},
	  "bind":[{"kind":"question","ref":"Q3"}]}`), &c))
	w.land("bound", c)
	if n := w.count(`SELECT count(*) FROM agentrt.plan_node_binding WHERE plan_node_id = $1 AND bound_kind = 'question' AND bound_ref = 'Q3'`, stepA); n != 1 {
		t.Fatalf("%d bindings", n)
	}
	see := func(option string) {
		t.Helper()
		_, err := seenfacts.SeeQuestion(contractCtx, w.rt, "Q3", func(context.Context) (seenfacts.Question, bool) {
			return seenfacts.Question{ID: "Q3", Status: seenfacts.QuestionAnswered, Option: option, Text: "yes", AnsweredBy: ownerActor, AnsweredAt: "2026-11-18T00:00:00Z"}, true
		})
		must(t, err)
	}
	if got := w.evaluate(stepA); got != "" {
		t.Fatalf("moved with no fact: %q", got)
	}
	see("B")
	if got := w.evaluate(stepA); got != "" {
		t.Fatalf("option B answered option A's atom: %q", got)
	}
	see("A")
	if got := w.evaluate(stepA); got != "done" || w.node(stepA).state != "done" {
		t.Fatalf("moved to %q, state %s", got, w.node(stepA).state)
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanDecisionsPage: more proposals waiting than a pass reads at once must not keep a later answered one from being decided; the wrong outcome is a card the Owner answered that never takes effect while older ones sit open.
func TestContractDecisionPassReachesEveryProposal(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	var last map[string]any
	for i := range 101 {
		out, err := w.propose(fmt.Sprint("wait-", i), retitle(planRoot, fmt.Sprint("title ", i)))
		must(t, err)
		last = out
	}
	w.desk.answer(w.questionOf(last), optionRefuse)
	if n := w.decide(); n != 1 || w.decisionOf(last) != "refused" {
		t.Fatalf("decided %d: %q", n, w.decisionOf(last))
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestPlanChangeQueuedUngatedRuleIsRefusedOnApply (R5:5.5-15 review round 1): a proposal already on the ledger when the sign-off gate shipped is checked again where the rule is written; the wrong outcome is the Owner's approval of a rule change standing in for his sign-off on the step.
func TestContractQueuedRuleIsRefusedWhenApplied(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	var proposal int64
	must(t, eventlog.InTx(contractCtx, w.rt, func(tx pgx.Tx) error {
		ev, err := eventlog.Append(contractCtx, tx, eventlog.Event{Stream: "plan:R2", Type: "plan_change_proposed", Actor: "controller",
			Body: map[string]any{"changes": []any{map[string]any{"op": "set_done_rule", "node": stepB, "rule": map[string]any{"step_landed": map[string]any{}}}},
				"reason": "queued before the gate", "standing": false, "question_id": "q_queued"}})
		proposal = ev.EventID
		return err
	}))
	w.desk.answer("q_queued", optionApply)
	w.decide()
	out := map[string]any{"proposal_event_id": proposal}
	if got := w.decisionOf(out); got != "failed" || !strings.Contains(w.decisionError(out), "R5:5.5-15") {
		t.Fatalf("decided %q: %s", got, w.decisionError(out))
	}
	if rule := w.node(stepB).rule; rule != "{}" {
		t.Fatalf("the queued rule was written: %s", rule)
	}
}

// Origin: 2026-11-18, TD-582: the contract version of store's TestDecisionAgainstRuleLoop (review 180 A3): a decision and a writer holding the node and then taking the plan's stream must not deadlock, because the decision takes the node before the stream; the wrong outcome is a decision pass killed by the database or a lost change.
func TestContractDecisionAgainstARowHeld(t *testing.T) {
	t.Parallel()
	w := newPlanWorld(t)
	out, err := w.propose("held", retitle(item1, "renamed"))
	must(t, err)
	w.desk.answer(w.questionOf(out), optionApply)
	tx, err := w.rt.Begin(contractCtx)
	must(t, err)
	defer func() { _ = tx.Rollback(contractCtx) }()
	_, err = tx.Exec(contractCtx, `SELECT 1 FROM agentrt.plan_node WHERE plan_node_id = $1 FOR UPDATE`, item1)
	must(t, err)
	decided := make(chan error, 1)
	go func() {
		_, derr := planstore.DecidePlanChanges(contractCtx, w.rt, w.desk.question, nil)
		decided <- derr
	}()
	deadline := time.Now().Add(10 * time.Second)
	for w.count(`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND position('plan_node' in query) > 0`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the decision never waited on the row; the case proves nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, err = eventlog.Append(contractCtx, tx, eventlog.Event{Stream: "plan:R2", Type: "plan_changed", Actor: "controller", Body: map[string]any{"change": "loop"}})
	must(t, err)
	must(t, tx.Commit(contractCtx))
	must(t, <-decided)
	if got := w.node(item1).title; got != "renamed" || w.decisionOf(out) != "applied" {
		t.Fatalf("title %q, decided %q", got, w.decisionOf(out))
	}
}
