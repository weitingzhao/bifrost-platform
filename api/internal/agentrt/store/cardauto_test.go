package store_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy/rptest"
)

// cardStore is a question store as the card-auto-answer rule sees it: cards
// asked (with when, and the option they recommend), open until answered.
type cardStore struct {
	mu      sync.Mutex
	cards   map[string]*storedCard
	order   []string
	answers int
	acked   map[string]int
}

type storedCard struct {
	card                     store.RuntimeCard
	status, option, by, text string
}

func (s *cardStore) put(c store.RuntimeCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cards == nil {
		s.cards, s.acked = map[string]*storedCard{}, map[string]int{}
	}
	s.cards[c.ID] = &storedCard{card: c, status: "open"}
	s.order = append(s.order, c.ID)
}

// ask makes a card asked now; created moves it back.
func (s *cardStore) ask(_ context.Context, c store.Card) (string, error) {
	id := "q_" + c.Key[:16]
	s.mu.Lock()
	_, have := s.cards[id]
	s.mu.Unlock()
	if !have {
		rec := ""
		if c.Recommended > 0 {
			rec = string(rune('a' + c.Recommended - 1))
		}
		s.put(store.RuntimeCard{ID: id, TaskID: c.TaskID, Recommended: rec, CreatedAt: time.Now()})
	}
	return id, nil
}

func (s *cardStore) aged(id string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards[id].card.CreatedAt = time.Now().Add(-d)
}

func (s *cardStore) open(context.Context) ([]store.RuntimeCard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.RuntimeCard
	for _, id := range s.order {
		if c := s.cards[id]; c.status == "open" {
			out = append(out, c.card)
		}
	}
	return out, nil
}

func (s *cardStore) answer(_ context.Context, id, option, rule string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.cards[id]
	c.status, c.option, c.by = store.QuestionAnswered, option, rule
	s.answers++
	return nil
}

func (s *cardStore) ack(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked[id]++
	return nil
}

func (s *cardStore) question(_ context.Context, id string) (store.Question, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cards[id]
	if !ok {
		return store.Question{}, false
	}
	if c.status != store.QuestionAnswered {
		return store.Question{ID: id, Status: c.status, Options: 3}, true
	}
	return store.Question{ID: id, Status: c.status, Option: c.option, AnsweredBy: c.by, AnsweredAt: "2026-10-20T20:00:00Z", Options: 3}, true
}

func (s *cardStore) rule() store.CardRule {
	return store.CardRule{Cards: s.open, Answer: s.answer, Ack: s.ack}
}

// signCardAuto signs a card-auto-answer rule of kinds that waits after
// minutes.
func signCardAuto(t *testing.T, w *world, after, kinds string) {
	t.Helper()
	s := signer{t, rptest.NewKey(t)}
	ok(t, firstErr(s.sign(w, ruleText(rules.KindCardAuto, 1, rules.Initial, since, until, "after_minutes: "+after+"\nkinds: "+kinds+"\n"))))
}

// overBudget makes mission m, created 30 hours ago on a two-hour budget, with
// one ready task blocked for it: its card recommends closing the mission (R1).
func (w *world) overBudget(t *testing.T, m, needsOwner string) string {
	t.Helper()
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error {
		if _, err := store.CreateMission(ctx, tx, "controller", store.Mission{ID: m, Title: "goal", Goal: "a goal", Deadline: time.Now().Add(24 * time.Hour),
			Repos: []string{"bifrost-platform"}, Success: []string{store.GoalSuccess}, NeedsOwner: needsOwner}); err != nil {
			return err
		}
		return store.MoveMission(ctx, tx, "controller", m, "active", "test")
	}))
	id := w.ready(t, store.Task{MissionID: m, OffPlanReason: "a goal"})
	w.legacy(t, `UPDATE agentrt.mission SET created_at = clock_timestamp() - interval '30 hours',
	  budget_json = '{"max_attempts": 9, "max_wall_hours": 2}' WHERE mission_id = '`+m+`'`)
	return id
}

// TD-390 (Owner 10-20 「STG 用 A」): with no signed rule nothing is answered;
// with one, a blocked card the runtime asked is answered with the option it
// recommends once it has been open 30 minutes, as the rule, once; a card of
// a mission marked as needing the Owner, a card younger than the wait, a card
// with no recommendation, a planner's question the rule does not list and a
// card that is no runtime card of either kind are left open. The answer then
// does what the Owner's would: it closes the mission.
func TestAnswerByRule(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	answered := w.overBudget(t, "M-3", "")
	marked := w.overBudget(t, "M-4", "the walk tests the Owner's Close mission button")
	young := w.overBudget(t, "M-5", "")
	plain := w.ready(t, store.Task{}) // M-1: a card that recommends nothing
	ok(t, store.InTx(ctx, w.rt, func(tx pgx.Tx) error { return store.FailWorkForTest(ctx, tx, plain) }))
	plan := w.ready(t, store.Task{Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 3 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 4 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	card := map[string]string{}
	for _, id := range s.order {
		card[s.cards[id].card.TaskID] = id
		s.aged(id, 31*time.Minute)
	}
	s.aged(card[young], 29*time.Minute)
	if s.cards[card[plain]].card.Recommended != "" {
		t.Fatalf("a card of failed work with no check recommends %q", s.cards[card[plain]].card.Recommended)
	}
	s.put(store.RuntimeCard{ID: "q_plannerquestion1", TaskID: plan, Recommended: "a", CreatedAt: time.Now().Add(-time.Hour)})
	s.put(store.RuntimeCard{ID: "q_planchange00001", Recommended: "a", CreatedAt: time.Now().Add(-time.Hour)})

	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 0 || s.answers != 0 {
		t.Fatalf("with no rule: answered %d (%v)", n, err)
	}
	signCardAuto(t, w, "30", "[blocked]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 || s.answers != 1 {
		t.Fatalf("answered %d (%v), %d answers", n, err, s.answers)
	}
	got := s.cards[card[answered]]
	if got.status != store.QuestionAnswered || got.option != "c" || got.by != "rule card-auto-answer v1" {
		t.Fatalf("the card: %+v", got)
	}
	for _, id := range []string{card[marked], card[young], card[plain], "q_plannerquestion1", "q_planchange00001"} {
		if s.cards[id].status != "open" {
			t.Errorf("card %s was answered", id)
		}
	}
	var note string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'text' FROM agentrt.event WHERE event_type = 'note' AND mission_id = 'M-3' AND task_id = $1`,
		answered).Scan(&note))
	if !strings.HasPrefix(note, "rule card-auto-answer v1 answered question "+card[answered]+" with c, the option it recommends, after 3") {
		t.Fatalf("the note: %q", note)
	}
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 0 || s.answers != 1 {
		t.Fatalf("a second pass answered %d (%v)", n, err)
	}
	// The answer acts as the Owner's: the mission is closed, the answer kept
	// as the rule's and acknowledged.
	if n, err := store.DecideBlocked(ctx, w.rt, s.question, s.ack); err != nil || n != 1 {
		t.Fatalf("decided %d (%v)", n, err)
	}
	var by string
	ok(t, w.rt.QueryRow(ctx, `SELECT body_json->>'answered_by' FROM agentrt.event WHERE event_type = 'question_seen' AND body_json->>'question' = $1`,
		card[answered]).Scan(&by))
	if w.state(t, "mission", "M-3") != "stopped" || w.state(t, "task", answered) != "cancelled" || by != "rule card-auto-answer v1" ||
		s.acked[card[answered]] != 1 {
		t.Fatalf("M-3 %s, its task %s, answered by %q, acknowledged %d", w.state(t, "mission", "M-3"), w.state(t, "task", answered), by,
			s.acked[card[answered]])
	}
	if w.state(t, "mission", "M-4") != "active" {
		t.Fatalf("M-4, which needs the Owner, is %s", w.state(t, "mission", "M-4"))
	}
}

// A rule that lists planners' questions answers one with the option it
// recommends and acknowledges it (nothing else reads its answer); it still
// leaves a blocked card when blocked is not listed.
func TestAnswerByRulePlanQuestion(t *testing.T) {
	t.Parallel()
	w := newKernelWorld(t)
	w.goalMission(t, "M-3")
	plan := w.ready(t, store.Task{MissionID: "M-3", Role: "plan", Effect: "none", OffPlanReason: "a goal"})
	blocked := w.overBudget(t, "M-4", "")
	if n, err := store.BlockOverBudget(ctx, w.rt); err != nil || n != 1 {
		t.Fatalf("blocked %d (%v)", n, err)
	}
	var s cardStore
	if n, err := store.AskBlocked(ctx, w.rt, s.ask); err != nil || n != 1 {
		t.Fatalf("asked %d (%v)", n, err)
	}
	s.aged(s.order[0], time.Hour)
	s.put(store.RuntimeCard{ID: "q_plannerquestion1", TaskID: plan, Recommended: "b", CreatedAt: time.Now().Add(-time.Hour)})
	signCardAuto(t, w, "30", "[plan_question]")
	if n, err := store.AnswerByRule(ctx, w.rt, s.rule()); err != nil || n != 1 {
		t.Fatalf("answered %d (%v)", n, err)
	}
	if q := s.cards["q_plannerquestion1"]; q.status != store.QuestionAnswered || q.option != "b" || s.acked["q_plannerquestion1"] != 1 {
		t.Fatalf("the planner's question: %+v, acknowledged %d", q, s.acked["q_plannerquestion1"])
	}
	if s.cards[s.order[0]].status != "open" || w.state(t, "task", blocked) != "blocked" {
		t.Fatal("a blocked card was answered by a rule that does not list blocked cards")
	}
}
