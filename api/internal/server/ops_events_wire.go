package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actionrecord"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/opsevents"
	"github.com/weitingzhao/bifrost-platform/api/internal/checklist"
	"github.com/weitingzhao/bifrost-platform/api/internal/maintainer"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// wireOpsEvents has the platform's own work say on the bus what it came to
// (bus/opsevents). Saying it never holds up or fails the work.
func (s *Server) wireOpsEvents() {
	maintainer.OnHealthChange(func(id string, healthy bool) {
		safego.Go("bus-loop-health", func() { s.sayOpsEvent(context.Background(), opsevents.Loop(id, healthy)) })
	})
	s.audit.OnRecord(func(r actuation.AuditRecord) {
		safego.Go("bus-audit-line", func() { s.sayAuditLine(r) })
	})
	s.checklist.OnFlip(s.sayChecklistFlip)
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

// sayAuditLine writes an audit line on the bus (S02). The log stays what it is.
func (s *Server) sayAuditLine(r actuation.AuditRecord) {
	db := s.bus.db()
	if db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := actionrecord.AnnounceAudit(ctx, db, r.Actor, r.Action, r.Status); err != nil {
		slog.Warn("bus: audit line not said", "action", r.Action, "error", err)
	}
}

func (s *Server) sayOpsEvent(ctx context.Context, e opsevents.Event) {
	if err := s.announceOps(ctx, e); err != nil {
		slog.Warn("bus: ops event not said", "type", e.Type, "subject", e.Subject, "error", err)
	}
}

// sayChecklistFlip says a probed thing's new state. An error keeps the flip
// for the prober's next run.
func (s *Server) sayChecklistFlip(ctx context.Context, w checklist.Watched) error {
	const source = "checklist-prober"
	if w.Kind == checklist.WatchPlugin {
		return s.announceOps(ctx, opsevents.PluginStatus(w.Subject, w.Signal, source, w.Detail))
	}
	return s.announceOps(ctx, opsevents.EnvHealth(w.Subject, w.Signal, source, w.Detail))
}

func (s *Server) announceOps(ctx context.Context, e opsevents.Event) error {
	db := s.bus.db()
	if db == nil {
		return errors.New("the bus's database is not connected yet")
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return opsevents.Announce(cctx, db, e)
}
