package server

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
	"github.com/weitingzhao/bifrost-platform/api/internal/releases"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// mountLineage registers the lineage domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountLineage(r chi.Router) {
	r.Get("/lineage", s.lineage.HandleGet)
	r.Get("/lineage/branches", s.lineage.HandleBranches)
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleOperator))
		r.Put("/lineage/thread-title", s.lineage.HandleSetTitle)
	})
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleReporter))
		r.Put("/lineage/transcript-title", s.lineage.HandleReportTitle)
	})
}

// wireLineage builds the lineage handler from what it reads of other
// domains: Gitea access (delivery), the release records (delivery) and the
// thread titles. Lines of New until 2026-11-16, unchanged.
func (srv *Server) wireLineage(releasesSvc *releases.Service, titleStore *threadtitles.Store) {
	srv.lineage = lineage.NewHandler(lineage.NewService(func(ctx context.Context) (lineage.Access, error) {
		a, err := srv.delivery.GiteaAccess(ctx)
		// The agent organization's branches too (T36), as the result check reads them.
		return lineage.Access{Base: a.Base, Org: a.Org, User: a.User, Pass: a.Pass,
			AgentOrg: strings.TrimSpace(os.Getenv("AGENTRT_GIT_ORG"))}, err
	}).WithReleases(func(ctx context.Context) ([]lineage.Release, error) {
		recs, err := releasesSvc.List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]lineage.Release, 0, len(recs))
		for _, r := range recs {
			repos := make(map[string]string, len(r.Repos))
			for name, b := range r.Repos {
				repos[name] = b.SHA
			}
			out = append(out, lineage.Release{Run: r.Run, Lane: r.Lane, Env: r.Env, Deploys: r.Deploys, At: r.CompletedAt, Repos: repos})
		}
		return out, nil
	})).WithTitles(func(ctx context.Context) (lineage.ThreadTitles, error) {
		tt, err := titleStore.Load(ctx)
		out := lineage.ThreadTitles{Manual: map[string]lineage.TitleAt{}, Transcript: map[string]lineage.TitleAt{}}
		for k, v := range tt.Manual {
			out.Manual[k] = lineage.TitleAt{Title: v.Title, At: v.At}
		}
		for k, v := range tt.Transcripts {
			out.Transcript[k] = lineage.TitleAt{Title: v.Title, At: v.At}
		}
		return out, err
	}, func(ctx context.Context, session, title string) error {
		return titleStore.SetManual(ctx, session, title, time.Now().UTC())
	}).WithTitleReports(func(ctx context.Context, transcript, title string) error {
		return titleStore.ReportTranscript(ctx, transcript, title, time.Now().UTC())
	})
}
