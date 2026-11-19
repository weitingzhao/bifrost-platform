package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The late DB keeps an unavailable runtime retryable. Both processes can
// deliver; PostgreSQL, not a process-local flag, owns deduplication.
func (s *Server) wireThreadInbox(db eventlog.DB) {
	send := func(ctx context.Context, key, to, body string) error {
		return coord.PostConclusion(ctx, db, key, to, body)
	}
	s.approvals.SetThreadInbox(send)
	s.questionRuntime.Svc.SetThreadInbox(send)
}
