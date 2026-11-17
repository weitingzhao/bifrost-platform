package server

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
)

func (s *Server) agentRuntimePlanSources() planstore.PlanSources {
	var src planstore.PlanSources
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
			Main:     s.mainOf(nil),
		}
		src.History = func(ctx context.Context, repo, task, result, snapshot string) (landing.MainHistory, error) {
			return reader.History(ctx, repo, task, result, snapshot)
		}
	}
	return src
}
