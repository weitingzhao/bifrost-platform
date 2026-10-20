package store_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// staleOnStep makes M-1's step s1 hold a task over M-1's budget (M-1 created
// 30 hours ago on two hours), blocked for it with its card asked, which
// recommends closing M-1; M-2 is active and needs the Owner. It returns the
// task and its card.
func (w *world) staleOnStep(t *testing.T, s *cardStore) (string, string) {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.MoveMission(ctx, tx, "controller", "M-2", "active", "test") }))
	task := w.ready(t, store.Task{})
	w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '30 hours',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = 'M-1'`,
		`UPDATE agentrt.mission SET scope_json = scope_json || '{"needs_owner": "the walk tests his buttons"}' WHERE mission_id = 'M-2'`)
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := s.order[0]
	s.aged(card, time.Hour)
	if s.cards[card].card.Recommended != "c" {
		t.Fatalf("the card recommends %q", s.cards[card].card.Recommended)
	}
	return task, card
}

// rebind proposes moving step s1 and its tasks to M-2, as the Owner's plan
// change does, approves it, and returns the decision pass's count (0 while a
// task it moves is held) and the card's answers to decide it again with.
func (w *world) rebind(t *testing.T, db store.DB, key string) (int, *asker) {
	t.Helper()
	plan := &asker{}
	_, err := store.ProposePlanChange(ctx, db, wr("thread:s1"), key, root, "rebind", []store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, plan.ask)
	ok(t, err)
	plan.answer(plan.lastID(), store.QuestionAnswered, store.CardApplyKey)
	n, err := store.DecidePlanChanges(ctx, db, plan.question, nil)
	ok(t, err)
	return n, plan
}

// Review 442 A1: a blocked card's task moved to a mission that needs the
// Owner after the card was asked: the rule leaves its card alone.
func TestRuleLeavesACardWhoseTaskMoved(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var s cardStore
	task, card := w.staleOnStep(t, &s)
	if n, _ := w.rebind(t, w.rt, "k1"); n != 1 {
		t.Fatalf("the rebind applied %d", n)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 0 || s.cards[card].status != "open" {
		t.Fatalf("answered %d (%v): the card is %s", n, err, s.cards[card].status)
	}
	var mission string
	ok(t, w.rt.QueryRow(ctx, `SELECT mission_id FROM agentrt.task WHERE task_id = $1`, task).Scan(&mission))
	if mission != "M-2" || w.state(t, "task", task) != "blocked" || w.state(t, "mission", "M-2") != "active" {
		t.Fatalf("the task is %s in %s; M-2 %s", w.state(t, "task", task), mission, w.state(t, "mission", "M-2"))
	}
}

// Review 442 A1, the other order: while the rule writes its answer the card's
// task is held, so a plan change that would move it meanwhile is busy; once
// it moves, the answer closes nothing of either mission.
func TestRuleAnswerHoldsItsTask(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	var s cardStore
	task, card := w.staleOnStep(t, &s)
	signCardAuto(t, w, "30", "[blocked]")
	pool := w.db.Pool(t, w.db.Runtime) // another connection: the answer's transaction is on w.rt
	var during error
	s.during = func(string) {
		_, during = store.ProposePlanChange(ctx, pool, wr("thread:s1"), "k1", root, "rebind",
			[]store.PlanChange{{Op: "rebind_step", Node: s1, MissionID: "M-2"}}, (&asker{}).ask)
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 || !errors.Is(during, store.ErrBusy) {
		t.Fatalf("answered %d (%v); the plan change meanwhile: %v", n, err, during)
	}
	s.during = nil
	if n, _ := w.rebind(t, w.rt, "k2"); n != 1 {
		t.Fatalf("the rebind once the task is free applied %d", n)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	if o, _, _ := w.closedCard(t, card); o != store.OutcomeMoot || w.state(t, "mission", "M-1") != "active" || w.state(t, "mission", "M-2") != "active" ||
		w.state(t, "task", task) != "blocked" {
		t.Fatalf("the card closed %q; M-1 %s, M-2 %s, the task %s", o, w.state(t, "mission", "M-1"), w.state(t, "mission", "M-2"), w.state(t, "task", task))
	}
}

// Review 442 A2: a rule tightened while a pass is answering governs the cards
// after it: the pass reads the rule for each card, and a new version waits
// for the answer being written.
// Serial: it waits for the database's lock queue.
func TestRuleTightenedMidPass(t *testing.T) {
	w := newKernelWorld(t)
	first := w.overBudget(t, "M-3", "")
	second := w.overBudget(t, "M-4", "")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 2 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := map[string]string{}
	for _, id := range s.order {
		card[s.cards[id].card.TaskID] = id
		s.aged(id, 31*time.Minute)
	}
	if s.order[0] != card[first] {
		s.order[0], s.order[1] = s.order[1], s.order[0]
	}
	key := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(key.sign(w, ruleText(rules.KindCardAuto, 1, rules.Initial, since, until, "after_minutes: 30\nkinds: [blocked]\n"))))
	tightened := make(chan error, 1)
	pool := w.db.Pool(t, w.db.Runtime)
	s.during = func(id string) {
		if id != card[first] {
			return
		}
		go func() {
			text := ruleText(rules.KindCardAuto, 2, rules.Tighten, since, until, "after_minutes: 60\nkinds: [blocked]\n")
			_, err := store.SignRule(ctx, pool, store.Writer{Actor: "owner", Token: "admin"}, idOf(text), []byte(text),
				key.key.Sign(t, []byte(text), rules.Namespace), key.key.Fingerprint)
			tightened <- err
		}()
		select {
		case err := <-tightened:
			t.Fatalf("the tightening returned at once: %v", err)
		case <-time.After(300 * time.Millisecond):
		}
		w.waitFor(t, "pg_advisory_xact_lock(", time.Now().Add(10*time.Second))
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	ok(t, returned(t, tightened))
	if s.cards[card[first]].status != store.QuestionAnswered || s.cards[card[second]].status != "open" {
		t.Fatalf("the first card %s, the second %s; want the second left to the tightened rule", s.cards[card[first]].status,
			s.cards[card[second]].status)
	}
}

// Review 442 A3: what a rule's answer does is the rule's, never the Owner's:
// one more round (and the budget it grows), not doing a task, closing a
// mission each name the rule, and the closed card says who answered.
func TestRuleAnswerIsRecordedAsTheRules(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	closed := w.overBudget(t, "M-3", "")
	w.goalMission(t, "M-4")
	retried := w.ready(t, store.Task{MissionID: "M-4", OffPlanReason: "a goal"})
	w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '3 hours',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = 'M-4'`)
	w.goalMission(t, "M-5")
	dropped := w.ready(t, store.Task{MissionID: "M-5", OffPlanReason: "a goal"})
	w.failRound(t, dropped, "h1", "h2")
	w.failRound(t, dropped, "h3", "h4")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 2 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 3 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := map[string]string{}
	for _, id := range s.order {
		card[s.cards[id].card.TaskID] = id
		s.aged(id, time.Hour)
	}
	for task, want := range map[string]string{closed: "c", retried: "a", dropped: "b"} {
		if got := s.cards[card[task]].card.Recommended; got != want {
			t.Fatalf("task %s's card recommends %q, want %q", task, got, want)
		}
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 3 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 3 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	const rule = "the rule card-auto-answer v1"
	for task, want := range map[string]string{
		closed:  rule + " closed mission M-3 on the card of task " + closed,
		retried: rule + " gave it one more round (question " + card[retried] + ")",
		dropped: rule + " chose not to do it (question " + card[dropped] + ")",
	} {
		if r := w.lastMove(t, task); !strings.HasPrefix(r, want) {
			t.Errorf("task %s moved because %q; want %q", task, r, want)
		}
		var by string
		ok(t, w.rt.QueryRow(ctx, `SELECT COALESCE(body_json->>'by', '') FROM agentrt.event WHERE event_type = 'owner_wait_closed' AND body_json->>'question_id' = $1`,
			card[task]).Scan(&by))
		if by != "rule card-auto-answer v1" {
			t.Errorf("task %s's card closed by %q", task, by)
		}
	}
	if m := w.lastMissionMove(t, "M-3"); !strings.HasPrefix(m["reason"].(string), rule+" closed it on the card of task") {
		t.Errorf("M-3 moved because %q", m["reason"])
	}
	if a := w.amended(t, "M-4"); len(a) != 1 || !strings.HasPrefix(a[0], "controller "+retried+" ") || !strings.Contains(a[0], rule+" gave task "+retried) ||
		strings.Contains(a[0], "Owner") {
		t.Errorf("M-4 amended %v", a)
	}
	var owner int
	ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE actor_id = 'owner' AND mission_id IN ('M-3','M-4','M-5')`).Scan(&owner))
	if owner != 0 {
		t.Errorf("%d events of the rule's answers are the Owner's", owner)
	}
}

// Review 442 B1: what a pass leaves of a rule's answer is finished by the
// next: a planner question's acknowledgement that failed, and a note the
// answer's transaction did not keep; each once.
func TestRuleAnswerFinishedLater(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	plan := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	other := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	var s cardStore
	s.put(store.RuntimeCard{ID: "q_plannerquestion1", TaskID: plan, Recommended: "b", CreatedAt: time.Now().Add(-time.Hour)})
	// Answered by the rule in a transaction that did not keep its note.
	s.put(store.RuntimeCard{ID: "q_plannerquestion2", TaskID: other, Recommended: "a", CreatedAt: time.Now().Add(-time.Hour)})
	s.cards["q_plannerquestion2"].status, s.cards["q_plannerquestion2"].option, s.cards["q_plannerquestion2"].by = store.QuestionAnswered, "a", "rule card-auto-answer v1"
	signCardAuto(t, w, "30", "[plan_question]")
	s.ackErr = errors.New("the question store did not answer in time")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); n != 1 || err == nil {
		t.Fatalf("the first pass: answered %d (%v); want one answer and the failed acknowledgement", n, err)
	}
	for range 2 {
		if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 0 {
			t.Fatalf("a later pass: answered %d (%v)", n, err)
		}
	}
	notes := func(q string) int {
		var n int
		ok(t, w.rt.QueryRow(ctx, `SELECT count(*) FROM agentrt.event WHERE event_type = 'note' AND body_json->>'text' LIKE '% answered question ' || $1 || ' %'`,
			q).Scan(&n))
		return n
	}
	for _, q := range []string{"q_plannerquestion1", "q_plannerquestion2"} {
		if s.acked[q] != 1 || notes(q) != 1 {
			t.Errorf("%s: acknowledged %d times, %d notes", q, s.acked[q], notes(q))
		}
	}
}
