package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

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

// catalogScanAhead starts the scanner in the workers: every repository of the
// Gitea organization the releases clone from (the tenant's own list: the
// platform names none of them), the CI scorecard's until the first listing.
func (s *Server) catalogScanAhead(ctx context.Context) {
	if s.delivery == nil {
		return
	}
	fallback := make([]string, 0, len(ciscore.Repos))
	for _, r := range ciscore.Repos {
		fallback = append(fallback, r.Repo)
	}
	s.catalogScanner().Run(ctx, s.catalogRepos, fallback)
}

// catalogRepos lists the organization's repositories that are neither
// archived nor empty, with the delivery service's read-only credential.
func (s *Server) catalogRepos(ctx context.Context) ([]string, error) {
	acc, err := gitAccess(s.delivery.GiteaAccess)(ctx)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 20 * time.Second}
	var out []string
	for page := 1; page <= 10; page++ {
		u := strings.TrimRight(acc.Base, "/") + "/api/v1/orgs/" + url.PathEscape(acc.Org) + "/repos?limit=50&page=" + strconv.Itoa(page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		if acc.User != "" {
			req.SetBasicAuth(acc.User, acc.Pass)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		var raw []struct {
			Name     string `json:"name"`
			Archived bool   `json:"archived"`
			Empty    bool   `json:"empty"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw)
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("gitea repos: status %d", resp.StatusCode)
		}
		if err != nil {
			return nil, err
		}
		for _, r := range raw {
			if !r.Archived && !r.Empty {
				out = append(out, r.Name)
			}
		}
		if len(raw) < 50 {
			break
		}
	}
	sort.Strings(out)
	return out, nil
}

// mountCatalog: the pool is read-only and names repositories and paths, so
// viewer and above.
func (s *Server) mountCatalog(r chi.Router) {
	viewer := r.With(s.auth.Require(actuation.RoleViewer))
	viewer.Method(http.MethodGet, "/catalog/unperceived", catalog.Route{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/owner", catalog.OwnerRoute{Scanner: s.catalogScanner()})
	viewer.Method(http.MethodGet, "/catalog/index", catalog.IndexRoute{Scanner: s.catalogScanner()})
}
