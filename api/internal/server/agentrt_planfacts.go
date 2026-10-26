package server

import (
	"context"
	"net/http"
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
		client := &http.Client{Timeout: 20 * time.Second}
		src.History = func(ctx context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
			a, err := s.delivery.GiteaAccess(ctx)
			if err != nil {
				return landing.MainHistory{State: landing.Unknown}, err
			}
			return lineage.PlanHistory(ctx, lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, client, repo, task, result, snapshot, 200)
		}
	}
	return src
}
