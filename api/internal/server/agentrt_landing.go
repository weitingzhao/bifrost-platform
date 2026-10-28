package server

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Only the workers path calls this, and only an explicit STG opt-in enables
// the loop. Constructing adapters does not read credentials or contact services.
func (s *Server) wireLandingDrill(c *controller.Controller) {
	if c.Env != "stg" || os.Getenv("AGENTRT_LANDING_DRILL") != "on" {
		return
	}
	g, err := landinggit.FromEnv(c.Env)
	if err != nil {
		slog.Error("agentrt landing drill disabled", "error", err)
		return
	}
	if s.delivery == nil {
		slog.Error("agentrt landing drill disabled: delivery unavailable")
		return
	}
	c.DrillGit = g
	c.Drill = &controller.DrillSources{Facts: controller.DrillGitFacts(g), Main: func(ctx context.Context) (string, error) { return g.Ref(ctx, "refs/heads/main") }, CI: s.delivery.LandingCI, EnsureCI: s.delivery.EnsureLandingCI, WindowBlocked: s.delivery.LandingWindowBlocked, Delete: g.Delete,
		MainEvidence: func(ctx context.Context, repo, r string) (store.MainEvidence, error) {
			if repo != "bifrost-platform" {
				return store.MainEvidence{}, errors.New("unsupported landing repo")
			}
			head, has, err := g.MainEvidence(ctx, r)
			return store.MainEvidence{Head: head, HasR: has}, err
		}}
}
