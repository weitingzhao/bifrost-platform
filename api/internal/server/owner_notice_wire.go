package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/ownernotice"
)

// sayOwnerNotice writes on the bus what the phone was just told
// (bus/ownernotice). The push has happened: a failure here is logged and
// changes nothing about it.
func (s *Server) sayOwnerNotice(ctx context.Context, n ownernotice.Notice) {
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ownernotice.Announce(cctx, s.agentRuntime.api.DB, n); err != nil {
		slog.Warn("bus: owner notice not said", "type", n.Type, "subject", n.Subject, "error", err)
	}
}
