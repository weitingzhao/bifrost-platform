package server

import (
	"context"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentthreads"
	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// ConsoleNeedsYou is the Console page a silent-thread push opens.
const ConsoleNeedsYou = "http://ops.bifrost.lan/#needs-you"

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
