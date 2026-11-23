package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// GET /api/v1/units/runs (cirunview.RunsRoute); this file hands it the cluster
// and the process's cache of answers (verification/assembly.RunsKept).

const unitRunsDefault = cirunview.RunsDefault

func (s *Server) handleUnitRuns(w http.ResponseWriter, r *http.Request) {
	cirunview.RunsRoute{RepoOK: giteaName.MatchString,
		Available: func() bool { return s.cluster != nil },
		Read: func(ctx context.Context, repo string, limit int) (cirunview.Answer, error) {
			return s.unitRunsKept.Read(ctx, s.landingCI(), time.Now(), repo, limit)
		}}.ServeHTTP(w, r)
}
