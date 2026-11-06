package server

import (
	"context"
	"log/slog"
	"os"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
)

// wireFullCI gives PROD's controller the daily full CI (controller/fullci.go):
// the landing CI's client creates and lists the runs, the platform's
// read-only Gitea identity reads main. AGENTRT_FULL_CI=off, another
// environment or no cluster wires none.
func (s *Server) wireFullCI(c *controller.Controller) {
	on, at, param, err := controller.FullCISettings(os.Getenv)
	if err != nil {
		slog.Error("agentrt full CI not started", "error", err)
	}
	if !on || c.Env != "prod" || c.DB == nil || s.cluster == nil {
		return
	}
	ci := s.landingCIClient()
	ci.FullScopeParam = param
	git := func() (*landinggit.Git, error) { return landinggit.ReaderFromEnv("bifrost-platform") }
	c.FullCI = &controller.FullCI{At: at, Runs: ci.FullRuns, Ensure: ci.EnsureFullRun, TestLog: ci.FullRunTestLog,
		Main: func(ctx context.Context) (string, error) {
			g, err := git()
			if err != nil {
				return "", err
			}
			return g.Ref(ctx, "refs/heads/main")
		},
		Landings: func(ctx context.Context, from, to string, max int) ([]controller.FullCILanding, error) {
			g, err := git()
			if err != nil {
				return nil, err
			}
			w, err := g.Open(ctx, to)
			if err != nil {
				return nil, err
			}
			defer w.Close()
			subjects, err := w.FirstParents(ctx, from, to, max)
			if err != nil {
				return nil, err
			}
			out := make([]controller.FullCILanding, len(subjects))
			for i, s := range subjects {
				out[i] = controller.FullCILanding{Commit: s.SHA, Subject: s.Subject}
			}
			return out, nil
		},
	}
}
