package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/bus/opsevents"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// wireOpsEvents has the platform's own work say on the bus what it came to
// (bus/opsevents). Saying it never holds up or fails the work.
func (s *Server) wireOpsEvents() {
	maintainer.OnHealthChange(func(id string, healthy bool) {
		safego.Go("bus-loop-health", func() { s.sayOpsEvent(context.Background(), opsevents.Loop(id, healthy)) })
	})
	if s.delivery != nil {
		s.delivery.OnMirrorSettled(func(ctx context.Context, repo, commit string, present bool) {
			s.sayOpsEvent(ctx, opsevents.Mirror(repo, commit, present))
		})
	}
	if s.dataclone != nil {
		s.dataclone.OnCloneFinished(func(ctx context.Context, jobID string, ok bool) {
			s.sayOpsEvent(ctx, opsevents.Job(jobID, ok))
		})
	}
}

func (s *Server) sayOpsEvent(ctx context.Context, e opsevents.Event) {
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := opsevents.Announce(cctx, s.agentRuntime.api.DB, e); err != nil {
		slog.Warn("bus: ops event not said", "type", e.Type, "subject", e.Subject, "error", err)
	}
}
