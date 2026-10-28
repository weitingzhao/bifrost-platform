package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Only the workers path calls this. The main reconciler's reader is wired
// in STG and PROD whatever the landing switches say (review 663 B-02):
// turning the switch off stops new claims and pushes, never the recovery of
// what was already pushed. A writing loop needs an explicit opt-in:
// AGENTRT_LANDING_DRILL=on on STG (the drill), or AGENTRT_LANDING_LIVE=on
// on PROD with the lander identity (S3), and the delivery adapters.
// Constructing adapters does not read credentials or contact services.
func (s *Server) wireLandingDrill(c *controller.Controller) {
	// denied settles from the database in every environment (S3 must 3).
	c.Reconcile = &controller.LandingReconcile{Repos: []string{"bifrost-platform"}}
	var g *landinggit.Git
	var err error
	switch c.Env {
	case "stg":
		g, err = landinggit.FromEnv(c.Env)
	case "prod":
		g, err = landinggit.LanderFromEnv(c.Env)
	default:
		return
	}
	if err != nil {
		// No Git identity: every read of main fails visibly (a landing's
		// evidence records unreadable), never a silent nil reader.
		unavailable := fmt.Errorf("main reader unavailable: %w", err)
		c.Reconcile.MainEvidence = func(context.Context, string, string, string, string) (store.MainEvidence, error) {
			return store.MainEvidence{}, unavailable
		}
		slog.Error("agentrt landing: no Git identity; main reads fail and no landing runs", "env", c.Env, "error", err)
		return
	}
	c.Reconcile.MainEvidence = mainEvidenceReader(g)
	write := c.Env == "stg" && os.Getenv("AGENTRT_LANDING_DRILL") == "on" || c.Env == "prod" && os.Getenv("AGENTRT_LANDING_LIVE") == "on"
	if !write {
		return
	}
	if s.delivery == nil {
		slog.Error("agentrt landing disabled: delivery unavailable", "env", c.Env)
		return
	}
	c.DrillGit = g
	c.Drill = &controller.DrillSources{Facts: controller.DrillGitFacts(g), Main: func(ctx context.Context) (string, error) { return g.Ref(ctx, "refs/heads/main") }, CI: s.delivery.LandingCI, EnsureCI: s.delivery.EnsureLandingCI, WindowBlocked: s.delivery.LandingWindowBlocked}
	if c.Env == "prod" {
		// A new live landing needs every reach the landing cards need (CUT
		// §11.6 v3 item 9): without them the Owner could not be asked.
		if !controller.LandingCardsComplete(c.LandingCards) {
			slog.Error("agentrt landing live disabled: the landing cards cannot reach the question store")
			c.Drill, c.DrillGit = nil, nil
			return
		}
		c.Live = true // land/prod branches are kept (B-05): no Delete
	} else {
		c.Drill.Delete = g.Delete
	}
}

// mainEvidenceReader reads main for the reconciler with the environment's
// Git identity.
func mainEvidenceReader(g *landinggit.Git) store.MainEvidenceFunc {
	return func(ctx context.Context, repo, root, a, r string) (store.MainEvidence, error) {
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
