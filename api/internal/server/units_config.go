package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/ciconfig"
)

// GET /api/v1/units/config?repo=…[&commit=…] is how CI is set up for one
// project, read from the declaration CI itself reads (declaration standard
// §6, the configuration view): whether the project has one, its modules, what
// each reads, and how many units and tasks that makes. Without commit it is
// main's head. A project with no declaration answers declared=false: it is
// one block and all of it runs every time. Nothing here is written by hand
// for the page; a module the declaration does not name is not in the answer.
// Read-only; viewer and above, as /units/plan.

// Storage and HTTP remain adapters; CI configuration projection owns its model.
func unitConfig(ctx context.Context, trees unitTreesFunc, repo, commit string) (ciconfig.Answer, error) {
	return ciconfig.Read(ctx, ciconfig.Trees(trees), repo, commit)
}

func (s *Server) handleUnitConfig(w http.ResponseWriter, r *http.Request) {
	repo, commit := strings.TrimSpace(r.URL.Query().Get("repo")), strings.TrimSpace(r.URL.Query().Get("commit"))
	if !giteaName.MatchString(repo) || strings.HasSuffix(repo, ".git") || (commit != "" && !commitID.MatchString(commit)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo is a repository name; commit, when given, a 40-character lowercase SHA"})
		return
	}
	if s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform does not read Gitea"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
	defer cancel()
	trees := s.unitTreesOf()
	if commit == "" {
		head, err := s.mainHead(ctx, repo)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		commit = head
	}
	out, err := unitConfig(ctx, trees, repo, commit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// mainHead is the commit main of repo is at, as Gitea says.
func (s *Server) mainHead(ctx context.Context, repo string) (string, error) {
	s.unitTreesOf()
	body, err := s.unitTrees.get(ctx, url.PathEscape(repo)+"/branches/main", 1<<20)
	var branch struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err == nil {
		err = json.Unmarshal(body, &branch)
	}
	if err != nil || !commitID.MatchString(branch.Commit.ID) {
		return "", fmt.Errorf("gitea: main of %s could not be read", repo)
	}
	return branch.Commit.ID, nil
}
