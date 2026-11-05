package server

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// ConsoleNeedsYou is where a silent-thread push opens: For you, at everything waiting
// (the Needs you page became that section, W-31 2.2b; the old hash still lands there).
const ConsoleNeedsYou = "http://ops.bifrost.lan/#you?section=waiting"

// LookFallback is the server-written title of a Look push whose composed
// text contains a hidden character. It is the verb and the record, not the
// session's own words. Thread silence and host loss use the same one.
const LookFallback = "Look · thread"

func agentThreadNotice(title, body, kind, id string) approvalnotify.Message {
	return approvalnotify.Message{
		Title: title, Message: body, ClickURL: ConsoleNeedsYou,
		Fallback: LookFallback, Kind: kind, ID: id,
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
		ids, err := store.ExternalIdentities(ctx, db)
		if err != nil {
			return nil, err
		}
		out := make([]agentthreads.Work, 0, len(ids))
		for _, x := range ids {
			out = append(out, agentthreads.Work{ID: x.ID, Root: x.Root, Node: x.Node, State: x.State, Host: x.Host, Vendor: x.Vendor, Session: x.Session, Epoch: int(x.Epoch)})
		}
		return out, nil
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
		out := make(map[string]string, len(tt.Transcripts))
		for id, e := range tt.Transcripts {
			out[id] = e.Title
		}
		return out, err
	}
	rec := agentthreads.NewRecorder(store, cfg)
	if role.RunsAPI() {
		rec.Start(context.Background(), agentthreads.FlushInterval)
	}
	s.agentThreads = agentthreads.NewHandler(rec, cfg, titles, s.audit)
	if role.RunsWorkers() && agentthreads.WatchWanted() {
		agentthreads.NewWatcher(store, cfg, func(ctx context.Context, title, body, kind, id string) error {
			return approvalnotify.Notify(ctx, agentThreadNotice(title, body, kind, id))
		}, titles).Start(context.Background(), agentthreads.WatchInterval)
	}
}
