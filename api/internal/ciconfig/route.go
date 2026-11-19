package ciconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Route is GET /api/v1/units/config?repo=…[&commit=…]: how CI is set up for one
// project, read from the declaration CI itself reads (declaration standard §6,
// the configuration view): whether the project has one, its modules, what each
// reads, and how many units and tasks that makes. Without commit it is main's
// head. A project with no declaration answers declared=false: it is one block
// and all of it runs every time. Nothing here is written by hand for the page;
// a module the declaration does not name is not in the answer. Read-only;
// viewer and above, as /units/plan.
type Route struct {
	RepoOK, CommitOK func(string) bool
	// Available says this platform reads Gitea.
	Available func() bool
	MainHead  func(ctx context.Context, repo string) (string, error)
	Read      func(ctx context.Context, repo, commit string) (Answer, error)
}

func (rt Route) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !rt.RepoOK(repo) || strings.HasSuffix(repo, ".git") || (commit != "" && !rt.CommitOK(commit)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name; commit, when given, a 40-character lowercase SHA"})
		return
	}
	if !rt.Available() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	if commit == "" {
		head, err := rt.MainHead(ctx, repo)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		commit = head
	}
	out, err := rt.Read(ctx, repo, commit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
