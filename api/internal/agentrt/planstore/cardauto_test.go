package planstore_test

import (
	"context"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
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
