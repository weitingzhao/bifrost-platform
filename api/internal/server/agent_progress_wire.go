package server

import (
	"context"
	"path/filepath"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentprogress"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

// wireAgentProgress serves the coordination progress document (W-62).
// Interim bridge (DESIGN §20.4): the coordinating thread publishes the
// document its local board already computes. Later the runtime computes it
// from events. The recorder coalesces those publishes and flushes every 10s
// so a ConfigMap is not written on every post.
func (s *Server) wireAgentProgress(dataDir string, role config.Role) {
	store := agentprogress.NewStore(filepath.Join(dataDir, agentprogress.StateRel))
	rec := agentprogress.NewRecorder(store)
	if role.RunsAPI() {
		rec.Start(context.Background(), agentprogress.FlushInterval)
	}
	s.agentProgress = agentprogress.NewHandler(rec)
}
