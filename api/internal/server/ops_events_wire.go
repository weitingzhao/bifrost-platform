package server

import (
	"context"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/opsevents"
	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/patrol"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// wireOpsEvents has the platform's own work say on the bus what it came to
// (bus/opsevents). Saying it never holds up or fails the work.
func (s *Server) wireOpsEvents() {
	bus := opsevents.Speaker{DB: s.bus.db}
	maintainer.OnHealthChange(func(id string, healthy bool) {
		safego.Go("bus-loop-health", func() { bus.Say(context.Background(), opsevents.Loop(id, healthy)) })
	})
	s.audit.OnRecord(func(r actuation.AuditRecord) {
		safego.Go("bus-audit-line", func() { actionrecord.SayAudit(s.bus.db, r.Actor, r.Action, r.Status) })
	})
	// An error keeps the flip for the prober's next run.
	s.checklist.OnFlip(func(ctx context.Context, w checklist.Watched) error {
		return bus.Announce(ctx, opsevents.Probed(w.Kind == checklist.WatchPlugin, w.Subject, w.Signal, w.Detail))
	})
	s.ibgateway.OnAutoRepair(func(ctx context.Context, ok bool, message string) {
		bus.Say(ctx, opsevents.AutoRepair(ok, message))
	})
	patrol.OnFix(func(ctx context.Context, item string, ok bool, detail string) {
		bus.Say(ctx, opsevents.PatrolFix(checklist.ViewerEnv(), item, ok, detail))
	})
	if s.delivery != nil {
		s.delivery.OnMirrorSettled(func(ctx context.Context, repo, commit string, present bool) {
			bus.Say(ctx, opsevents.Mirror(repo, commit, present))
		})
	}
	if s.dataclone != nil {
		s.dataclone.OnCloneFinished(func(ctx context.Context, jobID string, ok bool) {
			bus.Say(ctx, opsevents.Job(jobID, ok))
		})
	}
}
