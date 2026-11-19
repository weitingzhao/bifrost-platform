package server

import (
	"context"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/ownernotice"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/workregistration"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalhome"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// agentThreadNotice fills the message type with the owner domain's link and
// fallback. It stays here: agentthreads may not import approvalnotify, and
// approvalhome may not either (questions imports approvalhome and must not
// reach approvalnotify).
func agentThreadNotice(title, body, kind, id string) approvalnotify.Message {
	return approvalnotify.Message{
		Title: title, Message: body, ClickURL: approvalhome.ConsoleNeedsYou,
		Fallback: approvalhome.LookFallback, Kind: kind, ID: id,
	}
}

// wireSessionBindings joins the thread list and the agent runtime
// (R5:5.5-12 §3): a session's binding reads the runtime's external work
// (read when asked, so a database connected later is read), and the plan
// view's coverage counts the sessions. Called once both are wired.
func (s *Server) wireSessionBindings() {
	if s.agentThreads == nil {
		return
	}
	s.agentThreads.UseWorks(func(ctx context.Context) ([]agentthreads.Work, error) {
		if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
			return nil, errors.New("the agent runtime database is not configured")
		}
		db := s.agentRuntime.api.DB
		if late, ok := db.(agentrtapi.Ready); ok && !late.Ready() {
			return nil, errors.New("the agent runtime database is not connected yet")
		}
		ids, err := workregistration.Identities(ctx, db)
		if err != nil {
			return nil, err
		}
		return agentthreads.WorksFromIdentities(ids), nil
	})
	if s.agentRuntime != nil && s.agentRuntime.api != nil {
		s.agentRuntime.api.Sessions = func(ctx context.Context) (any, error) { return s.agentThreads.SessionCoverage(ctx) }
	}
}

// wireAgentThreads serves the heartbeat routes and, in the PROD workers role,
// starts the silent-thread watch (W-54).
func (s *Server) wireAgentThreads(dataDir string, role config.Role, titleStore *threadtitles.Store) {
	cfg := agentthreads.ConfigFromEnv()
	store := agentthreads.NewStore(filepath.Join(dataDir, "agent-threads"))
	titles := func(ctx context.Context) (map[string]string, error) {
		tt, err := titleStore.Load(ctx)
		return threadtitles.TranscriptNames(tt), err
	}
	rec := agentthreads.NewRecorder(store, cfg)
	if role.RunsAPI() {
		rec.Start(context.Background(), agentthreads.FlushInterval)
	}
	s.agentThreads = agentthreads.NewHandler(rec, cfg, titles, s.audit)
	if role.RunsWorkers() && agentthreads.WatchWanted() {
		agentthreads.NewWatcher(store, cfg, func(ctx context.Context, title, body, kind, id string) error {
			if err := approvalnotify.Notify(ctx, agentThreadNotice(title, body, kind, id)); err != nil {
				return err
			}
			if n, ok := ownernotice.Liveness(kind, id); ok {
				s.sayOwnerNotice(ctx, n)
			}
			return nil
		}, titles).Start(context.Background(), agentthreads.WatchInterval)
	}
}
