package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landingci"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// GET /api/v1/units/runs?repo=…[&limit=…] is the data of the Ops run view:
// the newest CI runs of one project, newest first, each with which modules
// ran and why (the changes task's run-modules and reason), the run whose
// verdict it reused when it ran nothing itself, whether it is a daily full
// run, and how long each task took and waited. It is read from the runs in
// the cluster; the project is the value of their bifrost.io/repo label.
// Read-only; viewer and above, as /units/config.
//
// The answer of one project is kept for cirunview.Keep: the page asks again on
// every visit, and a list of a project's runs with their tasks is two large
// reads of the cluster (incident #63, 2026-11-11: a page's reads starved the
// platform).

const (
	unitRunsDefault = 30
	unitRunsMax     = 100
)

type unitRunsAnswer = cirunview.Answer

type unitRunsCache struct{ cache cirunview.Cache }

func (c *unitRunsCache) unitRuns(ctx context.Context, read func(context.Context, string, int) ([]landingci.RunView, error), now time.Time, repo string, limit int) (unitRunsAnswer, error) {
	return c.cache.Read(ctx, read, now, repo, limit)
}

func (s *Server) handleUnitRuns(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	limit := unitRunsDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > unitRunsMax {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is 1 to " + strconv.Itoa(unitRunsMax)})
			return
		}
		limit = n
	}
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name"})
		return
	}
	if s.cluster == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read the cluster"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := s.unitRunsKept.unitRuns(ctx, s.landingCIClient().RecentRuns, time.Now(), repo, limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
