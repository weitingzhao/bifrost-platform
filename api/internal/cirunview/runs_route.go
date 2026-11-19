package cirunview

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// How many runs GET /units/runs answers with, unless asked, and at most.
const (
	RunsDefault = 30
	runsMax     = 100
)

// RunsRoute is GET /api/v1/units/runs?repo=…[&limit=…], the data of the Ops run
// view: the newest CI runs of one project, newest first, each with which
// modules ran and why (the changes task's run-modules and reason), the run
// whose verdict it reused when it ran nothing itself, whether it is a daily
// full run, and how long each task took and waited. It is read from the runs in
// the cluster; the project is the value of their bifrost.io/repo label.
// Read-only; viewer and above, as /units/config.
//
// The answer of one project is kept for Keep: the page asks again on every
// visit, and a list of a project's runs with their tasks is two large reads of
// the cluster (incident #63, 2026-11-11: a page's reads starved the platform).
type RunsRoute struct {
	RepoOK func(string) bool
	// Available says this platform reads the cluster.
	Available func() bool
	Read      func(ctx context.Context, repo string, limit int) (Answer, error)
}

func (rt RunsRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo := strings.TrimSpace(r.URL.Query().Get("repo"))
	limit := RunsDefault
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > runsMax {
			writeRouteJSON(w, http.StatusBadRequest, map[string]string{"error": "limit is 1 to " + strconv.Itoa(runsMax)})
			return
		}
		limit = n
	}
	if !rt.RepoOK(repo) || strings.HasSuffix(repo, ".git") {
		writeRouteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name"})
		return
	}
	if !rt.Available() {
		writeRouteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read the cluster"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	out, err := rt.Read(ctx, repo, limit)
	if err != nil {
		writeRouteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeRouteJSON(w, http.StatusOK, out)
}

func writeRouteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
