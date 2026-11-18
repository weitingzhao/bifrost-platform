package store_test

import (
	"context"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/seenfacts"
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
	card                     planstore.RuntimeCard
	status, option, by, text string
	read                     bool
}

func (s *cardStore) put(c planstore.RuntimeCard) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cards == nil {
		s.cards, s.acked = map[string]*storedCard{}, map[string]int{}
	}
	s.cards[c.ID] = &storedCard{card: c, status: "open"}
	s.order = append(s.order, c.ID)
}

// ask makes a card asked now; created moves it back.
func (s *cardStore) ask(_ context.Context, c planstore.Card) (string, error) {
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
		s.put(planstore.RuntimeCard{ID: id, TaskID: c.TaskID, Recommended: rec, CreatedAt: time.Now(), Options: keys})
	}
	return id, nil
}

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
