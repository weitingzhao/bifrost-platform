package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// cardStore is a question store as the card-auto-answer rule sees it: cards
// asked (with when, and the option they recommend), open until answered.
type cardStore struct {
	mu      sync.Mutex
	cards   map[string]*storedCard
	order   []string
	answers int
	acked   map[string]int
	during  func(id string) // runs as an answer is written, before it lands
	after   func(id string) // runs once an answer has landed, before the rule's caller hears so
	ackErr  error           // what the next acknowledgement answers
}

type storedCard struct {
	card                     store.RuntimeCard
	status, option, by, text string
	read                     bool
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
		// The question store keys the options a, b, … in order.
		var keys []string
		for i := range c.Options {
			keys = append(keys, string(rune('a'+i)))
		}
		s.put(store.RuntimeCard{ID: id, TaskID: c.TaskID, Recommended: rec, CreatedAt: time.Now(), Options: keys})
	}
	return id, nil
}

func (s *cardStore) answer(ctx context.Context, id, option, rule string, until time.Time, why string) error {
	if s.during != nil {
		s.during(id)
	}
	s.mu.Lock()
	if !time.Now().Before(until) {
		s.mu.Unlock()
		return errors.New("the rule no longer holds")
	}
	c := s.cards[id]
	if option != c.card.Recommended && why == "" {
		s.mu.Unlock()
		return errors.New("the question store answered 409: a rule answers with the option the question recommends, or says why it takes another")
	}
	c.status, c.option, c.by, c.text = store.QuestionAnswered, option, rule, why
	s.answers++
	s.mu.Unlock()
	if s.after != nil {
		s.after(id)
	}
	return nil
}

func (s *cardStore) ack(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ackErr; err != nil {
		s.ackErr = nil
		return err
	}
	s.acked[id]++
	s.cards[id].read = true
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
	return store.Question{ID: id, Status: c.status, Option: c.option, Text: c.text, AnsweredBy: c.by, AnsweredAt: "2026-10-20T20:00:00Z", Options: 3,
		Channel: "rule"}, true
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
