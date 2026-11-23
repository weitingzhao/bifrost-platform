package server

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	lineageassembly "github.com/weitingzhao/bifrost-platform/api/internal/lineage/assembly"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// wireLineage builds the lineage handler (lineage/assembly) from what it
// reads of other domains: Gitea access (delivery), the release records
// (delivery) and the thread titles. Lines of New until 2026-11-16, unchanged;
// the lineage's own part left for lineage/assembly on 2026-11-19, and this
// file keeps the binding to delivery.
func (srv *Server) wireLineage(releasesSvc *releases.Service, titleStore *threadtitles.Store) {
	srv.lineage = lineageassembly.Wire(func(ctx context.Context) (lineageassembly.Gitea, error) {
		a, err := srv.delivery.GiteaAccess(ctx)
		return lineageassembly.Gitea{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass}, err
	}, func(ctx context.Context) ([]lineageassembly.Release, error) {
		recs, err := releasesSvc.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]lineageassembly.Release, 0, len(recs))
		for _, r := range recs {
			repos := make(map[string]string, len(r.Repos))
			for name, b := range r.Repos {
				repos[name] = b.SHA
			}
			out = append(out, lineageassembly.Release{Run: r.Run, Lane: r.Lane, Env: r.Env, Deploys: r.Deploys, At: r.CompletedAt, Repos: repos})
		}
		return out, nil
	}, titleStore)
}

// mountLineage hands the lineage domain's routes (lineage/assembly, since
// 2026-11-19) the handler and the gates the platform lends them.
func (s *Server) mountLineage(r chi.Router) {
	lineageassembly.Mount(r, s.lineage, lineageassembly.Gates{
		Operator: s.auth.Require(actuation.RoleOperator),
		Reporter: s.auth.Require(actuation.RoleReporter),
	})
}
