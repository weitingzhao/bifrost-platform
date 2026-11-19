package planstore_test

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// seenCards is a question store that keeps each card as the runtime asked
// it, so a test can read it as the Owner sees it.
type seenCards struct {
	cardStore
	asked map[string]planstore.Card
}

func (s *seenCards) ask(ctx context.Context, c planstore.Card) (string, error) {
	id, err := s.cardStore.ask(ctx, c)
	if err == nil {
		s.mu.Lock()
		if s.asked == nil {
			s.asked = map[string]planstore.Card{}
		}
		s.asked[id] = c
		s.mu.Unlock()
	}
	return id, err
}
