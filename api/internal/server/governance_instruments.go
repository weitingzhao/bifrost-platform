package server

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/instruments"
)

// GET /api/v1/governance/instruments: the laws, the Skills and the MCP tools
// the source at main declares (package instruments), for the Crew page.

// instrumentSource reads main through the same Gitea reader as the unit plan:
// its blob cache is by content id, so it cannot serve an older file. It lists
// a tree without giteaTrees.tree, which would also fetch every Go file of the
// repository on a cold process; this answer needs a few dozen files.
type instrumentSource struct {
	trees *giteaTrees
	head  func(ctx context.Context, repo string) (string, error)
}

func (i instrumentSource) MainHead(ctx context.Context, repo string) (string, error) {
	return i.head(ctx, repo)
}

func (i instrumentSource) Files(ctx context.Context, repo, commit string) (map[string]string, error) {
	listing, err := commitTree(func(u string, limit int64) ([]byte, error) { return i.trees.get(ctx, u, limit) },
		url.PathEscape(repo)+"/git/trees/"+commit)
	if err != nil {
		return nil, err
	}
	files := make(map[string]string, len(listing))
	for p, e := range listing {
		if e.Type == "blob" {
			files[p] = e.SHA
		}
	}
	return files, nil
}

func (i instrumentSource) Blob(ctx context.Context, repo, id string) ([]byte, error) {
	return i.trees.blob(ctx, repo, id)
}

func (s *Server) handleInstruments(w http.ResponseWriter, r *http.Request) {
	if s.delivery == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "gitea access is not available on this platform"})
		return
	}
	s.instrumentsOnce.Do(func() {
		s.unitTreesOf()
		s.instruments = instruments.New(instrumentSource{trees: s.unitTrees, head: s.giteaMainHead})
	})
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	report, err := s.instruments.Read(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the instruments could not be read in time"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}
