package headrelease

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ReleasedRoute is GET /api/v1/delivery/released?repo=…&commit=…: for every
// deliverable of deliverables.yaml that holds the repository, what its
// environment runs and whether that has the commit: so a line asks "is my
// commit released" instead of guessing a run's name (ci-feedback 2026-11-13).
// Read-only; viewer and above.
type ReleasedRoute struct {
	RepoOK, CommitOK func(string) bool
	// World is one pass's world, nil when the delivery service or the evidence
	// ledger is not connected.
	World func() *Live
	// ChangedFiles fails when from is not in to's history, and when it cannot
	// tell (the mirror is down, the walk is too long): neither is "released".
	ChangedFiles func(ctx context.Context, repo, from, to string) ([]string, error)
}

func (rt ReleasedRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !rt.RepoOK(repo) || strings.HasSuffix(repo, ".git") || !rt.CommitOK(commit) {
		writeRouteJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name, commit a 40-character lowercase SHA"})
		return
	}
	world := rt.World()
	if world == nil {
		writeRouteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the delivery service or the evidence ledger is not connected"})
		return
	}
	table, _, err := LoadTable()
	if err != nil {
		writeRouteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	within := func(ctx context.Context, repo, ancestor, commit string) (bool, error) {
		if ancestor == commit {
			return true, nil
		}
		_, diffErr := rt.ChangedFiles(ctx, repo, ancestor, commit)
		return diffErr == nil, nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	released, err := WhereReleased(ctx, world, table, repo, commit, within)
	if err != nil {
		writeRouteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if released == nil {
		released = []Released{}
	}
	writeRouteJSON(w, http.StatusOK, map[string]any{"repo": repo, "commit": commit, "head_release": On(), "released": released})
}

func writeRouteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ProdRows are the table's two rows the PROD shadow reads: PROD and the source
// of its candidates; nil when the table lacks one.
func ProdRows(table Table) (prod, source *Deliverable) {
	for i := range table.Deliverables {
		switch table.Deliverables[i].Name {
		case ProdRowName:
			prod = &table.Deliverables[i]
		case ProdSourceName:
			source = &table.Deliverables[i]
		}
	}
	return prod, source
}

// ProdRepo is the repository whose commits a PROD release brings: the row's
// first commit not tested with another.
func ProdRepo(prod *Deliverable) string {
	for _, c := range prod.Commits {
		if c.TestedWith == "" {
			return c.Repo
		}
	}
	return ""
}
