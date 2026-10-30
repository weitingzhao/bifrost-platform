package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The late DB keeps an unavailable runtime retryable. Both processes can
// deliver; PostgreSQL, not a process-local flag, owns deduplication.
func (s *Server) wireThreadInbox(db store.DB) {
	send := func(ctx context.Context, key, to, body string) error {
		return store.PostConclusion(ctx, db, key, to, body)
	}
	s.approvals.SetThreadInbox(send)
	s.questionRuntime.svc.SetThreadInbox(send)
}
