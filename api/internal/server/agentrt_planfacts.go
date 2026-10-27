package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

func (s *Server) agentRuntimePlanSources(deployed store.Deployed) store.PlanSources {
	src := store.PlanSources{Deployed: func(ctx context.Context, app, env string) (string, map[string]any, bool) {
		// Existing actual-pod digest reader is for the platform only. Unknown
		// applications cannot borrow another application's healthy release.
		if app != "bifrost-platform" || deployed == nil {
			return "", nil, false
		}
		return deployed(ctx, env)
	}}
	if s.delivery != nil {
		// One reader for the process: it keeps each repo's main between passes.
		reader := &lineage.PlanHistoryReader{
			Access: func(ctx context.Context) (lineage.Access, error) {
				a, err := s.delivery.GiteaAccess(ctx)
				return lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass,
					AgentOrg: strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))}, err
			},
			Client:   &http.Client{Timeout: 20 * time.Second},
			MaxPages: 200,
		}
		src.History = func(ctx context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
			return reader.History(ctx, repo, task, result, snapshot)
		}
	}
	return src
}
