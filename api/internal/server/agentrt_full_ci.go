package server

import (
	"context"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"log/slog"
	"os"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/controller"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/fullci"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landinggit"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planpace"
	"github.com/weitingzhao/bifrost-platform/api/internal/kubemetrics"
)

// wireFullCI gives PROD's controller the daily full CI (agentrt/fullci):
// the landing CI's client creates and lists the runs, the platform's
// read-only Gitea identity reads main. AGENTRT_FULL_CI=off, another
// environment or no cluster wires none.
func (s *Server) wireFullCI(c *controller.Controller) {
	on, at, param, err := fullci.FullCISettings(os.Getenv)
	if err != nil {
		slog.Error("agentrt full CI not started", "error", err)
	}
	if !on || c.Env != "prod" || c.DB == nil || s.cluster == nil {
		return
	}
	ci := s.landingCIClient()
	ci.FullScopeParam = param
	git := func() (*landinggit.Git, error) { return landinggit.ReaderFromEnv("bifrost-platform") }
	// The full CI's day is the Owner's, in the zone of the briefs.
	c.FullCIZone = planpace.BriefZone
	// The PROD head-release loop starts a candidate's full CI with the same
	// runner when that is all the candidate waits on (headrelease fullCIFor).
	s.fullCIRun = func(ctx context.Context, revision string) (string, error) {
		day := time.Now().UTC()
		if loc, err := time.LoadLocation(planpace.BriefZone); err == nil {
			day = time.Now().In(loc)
		}
		return ci.EnsureFullRun(kubemetrics.WithSite(ctx, "landingci.full"), revision, day)
	}
	c.FullCI = &fullci.FullCI{At: at, Runs: func(ctx context.Context, since time.Time) ([]landingci.FullRun, error) {
		// A name for the platform's count of its own Kubernetes requests: these
		// loops were the 1,077 unnamed LISTs of ten minutes on PROD, 2026-11-17.
		return ci.FullRuns(kubemetrics.WithSite(ctx, "landingci.full"), since)
	}, Ensure: func(ctx context.Context, revision string, day time.Time) (string, error) {
		return ci.EnsureFullRun(kubemetrics.WithSite(ctx, "landingci.full"), revision, day)
	},
		// A red run's handling, once per red run: the failed TaskRuns are read
		// one by one (landingci.failure), and each landing since the last green
		// lists every run of the pipeline again (landingci.read, up to
		// FullCILandingsMax lists of all pages). Unnamed until 2026-11-17.
		TestLog: func(ctx context.Context, run string) (string, error) {
			return ci.FullRunTestLog(kubemetrics.WithSite(ctx, "landingci.failure"), run)
		},
		FailedTasks: func(ctx context.Context, run string) ([]string, error) {
			return ci.FullRunFailedTasks(kubemetrics.WithSite(ctx, "landingci.failure"), run)
		},
		RunModules: func(ctx context.Context, revision string) (string, string, error) {
			return ci.RunModules(kubemetrics.WithSite(ctx, "landingci.read"), revision)
		},
		Main: func(ctx context.Context) (string, error) {
			g, err := git()
			if err != nil {
				return "", err
			}
			return g.Ref(ctx, "refs/heads/main")
		},
		Landings: func(ctx context.Context, from, to string, max int) ([]fullci.FullCILanding, error) {
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
			out := make([]fullci.FullCILanding, len(subjects))
			for i, s := range subjects {
				out[i] = fullci.FullCILanding{Commit: s.SHA, Subject: s.Subject}
			}
			return out, nil
		},
	}
}
