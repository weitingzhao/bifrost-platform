package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// GET /api/v1/units/runs (cirunview.RunsRoute); this file hands it the cluster
// and the process's cache of answers.

const unitRunsDefault = cirunview.RunsDefault

type unitRunsAnswer = cirunview.Answer

type unitRunsCache struct{ cache cirunview.Cache }

func (c *unitRunsCache) unitRuns(ctx context.Context, read func(context.Context, string, int) ([]landingci.RunView, error), now time.Time, repo string, limit int) (unitRunsAnswer, error) {
	return c.cache.Read(ctx, read, now, repo, limit)
}

func (s *Server) handleUnitRuns(w http.ResponseWriter, r *http.Request) {
	cirunview.RunsRoute{RepoOK: giteaName.MatchString,
		Available: func() bool { return s.cluster != nil },
		Read: func(ctx context.Context, repo string, limit int) (cirunview.Answer, error) {
			return s.unitRunsKept.unitRuns(ctx, s.landingCIClient().RecentRuns, time.Now(), repo, limit)
		}}.ServeHTTP(w, r)
}
