package server

import (
	"context"
	"errors"
	"io/fs"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/catalog"
	"github.com/weitingzhao/bifrost-platform/api/internal/ciscore"
)

// The map's pool of what it cannot perceive (catalog, 0.2 map line,
// 2026-11-20): the workers scan main of each repository the CI scorecard
// knows every catalog.Every and keep the pool; GET /catalog/unperceived reads
// what was kept, never Gitea. This file hands catalog Gitea's tree reader.

func (s *Server) catalogScanner() *catalog.Scanner {
	s.catalogOnce.Do(func() {
		s.catalog = &catalog.Scanner{Source: s.catalogSource}
	})
	return s.catalog
}

func (s *Server) catalogSource(ctx context.Context, repo string) (catalog.Tree, error) {
	if s.delivery == nil {
		return catalog.Tree{}, errors.New("this platform does not read Gitea")
	}
	commit, err := s.mainHead(ctx, repo)
	if err != nil {
		return catalog.Tree{}, err
	}
	tree, err := s.unitTreesOf()(ctx, repo, commit)
	if err != nil {
		return catalog.Tree{}, err
	}
	var files []string
	for _, e := range tree.Walk("") {
		files = append(files, e.Path)
	}
	return catalog.Tree{Commit: commit, Files: files, Read: func(p string) ([]byte, bool, error) {
		if e, ok := tree.Lookup(p); !ok || e.Dir {
			return nil, false, nil
		}
		raw, err := tree.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return raw, err == nil, err
	}}, nil
}

// catalogScanAhead starts the scanner in the workers.
func (s *Server) catalogScanAhead(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	repos := make([]string, 0, len(ciscore.Repos))
	for _, r := range ciscore.Repos {
		repos = append(repos, r.Repo)
	}
	s.catalogScanner().Run(ctx, repos)
}

// mountCatalog: the pool is read-only and names repositories and paths, so
// viewer and above.
func (s *Server) mountCatalog(r chi.Router) {
	r.With(s.auth.Require(actuation.RoleViewer)).Method(http.MethodGet, "/catalog/unperceived", catalog.Route{Scanner: s.catalogScanner()})
}
