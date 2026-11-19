package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/windownotice"
)

// wireWindowNotices posts the release window's queue notices
// (windownotice.Post) through the runtime's database.
func (s *Server) wireWindowNotices(db eventlog.DB) {
	if s.delivery == nil {
		return
	}
	s.delivery.SetWindowNotifier(func(ctx context.Context, _ /* key */, to, body string) error {
		return windownotice.Post(ctx, db, to, body)
	})
}
