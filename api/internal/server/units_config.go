package server

import (
	"context"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/ciconfig"
)

// GET /api/v1/units/config (ciconfig.Route); this file hands it Gitea.

// Storage and HTTP remain adapters; CI configuration projection owns its model.
func unitConfig(ctx context.Context, trees unitTreesFunc, repo, commit string) (ciconfig.Answer, error) {
	return ciconfig.Read(ctx, ciconfig.Trees(trees), repo, commit)
}

func (s *Server) handleUnitConfig(w http.ResponseWriter, r *http.Request) {
	ciconfig.Route{RepoOK: giteaName.MatchString, CommitOK: commitID.MatchString,
		Available: func() bool { return s.delivery != nil },
		MainHead:  s.mainHead,
		Read: func(ctx context.Context, repo, commit string) (ciconfig.Answer, error) {
			return unitConfig(ctx, s.unitTreesOf(), repo, commit)
		}}.ServeHTTP(w, r)
}

// mainHead is the commit main of repo is at: the ledger's when gate-git
// watches repo, else Gitea's (mainOf).
func (s *Server) mainHead(ctx context.Context, repo string) (string, error) {
	return s.mainOf("units-config", s.giteaMainHead)(ctx, repo)
}

// giteaMainHead is the commit main of repo is at, as Gitea says.
func (s *Server) giteaMainHead(ctx context.Context, repo string) (string, error) {
	s.unitTreesOf()
	return s.unitTrees.MainHead(ctx, repo)
}
