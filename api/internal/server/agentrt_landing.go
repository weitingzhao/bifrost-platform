package server

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Only the workers path calls this, and only an explicit STG opt-in enables
// the loop. Constructing adapters does not read credentials or contact services.
func (s *Server) wireLandingDrill(c *controller.Controller) {
	// The main reconciler runs in every environment, before and apart from
	// the drill (S3 must 3 wiring): denied settles from the database; main
	// is read where Git is wired below. PROD reads main once the lander
	// identity is (S3).
	c.Reconcile = &controller.LandingReconcile{Repos: []string{"bifrost-platform"}}
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
	c.Drill = &controller.DrillSources{Facts: controller.DrillGitFacts(g), Main: func(ctx context.Context) (string, error) { return g.Ref(ctx, "refs/heads/main") }, CI: s.delivery.LandingCI, EnsureCI: s.delivery.EnsureLandingCI, WindowBlocked: s.delivery.LandingWindowBlocked, Delete: g.Delete}
	// The drill's Git reads main for the reconciler too (STG).
	c.Reconcile.MainEvidence = func(ctx context.Context, repo, root, a, r string) (store.MainEvidence, error) {
		if repo != "bifrost-platform" {
			return store.MainEvidence{}, errors.New("unsupported landing repo")
		}
		head, has, hist, err := g.MainEvidence(ctx, r, root, a)
		ev := store.MainEvidence{Head: head, HasR: has, Validity: "unknown"}
		switch hist.State {
		case landing.Effective:
			ev.Validity = "effective"
		case landing.Reverted:
			ev.Validity, ev.RevertedBy = "reverted", hist.RevertedBy
		}
		return ev, err
	}
}
