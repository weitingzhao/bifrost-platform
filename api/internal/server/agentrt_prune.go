package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

// The readers of the branch cleanup (TD-383, store/prune.go): which attempt
// branches a repository has, for the branches agentd is given to delete; and
// which tasks main holds, for when an accepted result is first seen there.

// agentRuntimeAgentHeads reads the attempt branches of a repository in the
// organization agent branches go to (AGENTRT_GIT_ORG); nil without one.
func (s *Server) agentRuntimeAgentHeads() agentrtapi.AgentHeads {
	org := strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))
	if org == "" || s.delivery == nil {
		return nil
	}
	return agentrtapi.AgentHeads(giteaAgentHeads(s.delivery.GiteaAccess, org, &http.Client{Timeout: 10 * time.Second}))
}

// mainTasksHorizon and mainTasksPages bound one read of a main: two months,
// 2000 commits.
const (
	mainTasksHorizon = 60 * 24 * time.Hour
	mainTasksPages   = 40
	mainTasksTTL     = 10 * time.Minute
)

// agentRuntimeMainTasks reads a repository's main where agentd clones it from
// (the delivery's organization) for Agentrt-Task trailers, each read kept for
// mainTasksTTL; nil without the delivery service.
func (s *Server) agentRuntimeMainTasks() func(context.Context, string) (map[string]string, bool) {
	if s.delivery == nil {
		return nil
	}
	access, client := s.delivery.GiteaAccess, &http.Client{Timeout: 20 * time.Second}
	return cachedMainTasks(func(ctx context.Context, repo string) (map[string]string, error) {
		a, err := access(ctx)
		if err != nil {
			return nil, err
		}
		return lineage.MainTasks(ctx, lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, client, repo,
			time.Now().Add(-mainTasksHorizon), mainTasksPages)
	}, mainTasksTTL)
}

// cachedMainTasks keeps each repository's last good read for ttl; a failed
// read is not kept (ok false, and the next pass reads again).
func cachedMainTasks(read func(context.Context, string) (map[string]string, error), ttl time.Duration) func(context.Context, string) (map[string]string, bool) {
	type entry struct {
		at    time.Time
		tasks map[string]string
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	return func(ctx context.Context, repo string) (map[string]string, bool) {
		mu.Lock()
		e, ok := cache[repo]
		mu.Unlock()
		if ok && time.Since(e.at) < ttl {
			return e.tasks, true
		}
		tasks, err := read(ctx, repo)
		if err != nil {
			return nil, false
		}
		mu.Lock()
		cache[repo] = entry{time.Now(), tasks}
		mu.Unlock()
		return tasks, true
	}
}
