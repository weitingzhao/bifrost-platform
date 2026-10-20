package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/agentrtapi"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

// The readers of the branch cleanup (TD-383, store/prune.go): which attempt
// branches a repository has, for the branches agentd is given to delete; and
// which results main holds, for when an accepted result is first seen there
// and whether it still stands.

// agentRuntimeAgentHeads reads the attempt branches of a repository in the
// organization agent branches go to (AGENTRT_GIT_ORG); nil without one.
func (s *Server) agentRuntimeAgentHeads() agentrtapi.AgentHeads {
	org := strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))
	if org == "" || s.delivery == nil {
		return nil
	}
	return agentrtapi.AgentHeads(giteaAgentHeads(s.delivery.GiteaAccess, org, &http.Client{Timeout: 10 * time.Second}))
}

// mainHorizon and mainPages bound one read of a main: two months, 2000
// commits; mainTTL is how long a read is kept.
const (
	mainHorizon = 60 * 24 * time.Hour
	mainPages   = 40
	mainTTL     = 10 * time.Minute
)

// agentRuntimeMainResults reads a repository's main where agentd clones it
// from (the delivery's organization) for the runtime's results that stand
// there (lineage.MainResults), each read kept for mainTTL; nil without the
// delivery service.
func (s *Server) agentRuntimeMainResults() store.MainResults {
	if s.delivery == nil {
		return nil
	}
	access, client := s.delivery.GiteaAccess, &http.Client{Timeout: 20 * time.Second}
	return cachedMainResults(func(ctx context.Context, repo string) (store.OnMain, error) {
		a, err := access(ctx)
		if err != nil {
			return nil, err
		}
		return lineage.MainResults(ctx, lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, client, repo,
			time.Now().Add(-mainHorizon), mainPages)
	}, mainTTL)
}

// cachedMainResults keeps each repository's last good read for ttl; a failed
// read is not kept (ok false, and the next pass reads again).
func cachedMainResults(read func(context.Context, string) (store.OnMain, error), ttl time.Duration) store.MainResults {
	type entry struct {
		at      time.Time
		results store.OnMain
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	return func(ctx context.Context, repo string) (store.OnMain, bool) {
		mu.Lock()
		e, ok := cache[repo]
		mu.Unlock()
		if ok && time.Since(e.at) < ttl {
			return e.results, true
		}
		results, err := read(ctx, repo)
		if err != nil {
			return nil, false
		}
		mu.Lock()
		cache[repo] = entry{time.Now(), results}
		mu.Unlock()
		return results, true
	}
}
