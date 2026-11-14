package store_test

import (
	"context"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// seenCards is a question store that keeps each card as the runtime asked
// it, so a test can read it as the Owner sees it.
type seenCards struct {
	cardStore
	asked map[string]store.Card
}

func (s *seenCards) ask(ctx context.Context, c store.Card) (string, error) {
	id, err := s.cardStore.ask(ctx, c)
	if err == nil {
		s.mu.Lock()
		if s.asked == nil {
			s.asked = map[string]store.Card{}
		}
		s.asked[id] = c
		s.mu.Unlock()
	}
	return id, err
}

// cardOf is the card asked for task.
func (s *seenCards) cardOf(t *testing.T, task string) string {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.asked {
		if c.TaskID == task {
			return id
		}
	}
	t.Fatalf("no card for task %s", task)
	return ""
}
