// Package assembly is the lineage domain's assembly: it builds the lineage
// handler around the thread titles and mounts the lineage's routes under
// /api/v1. Moved out of server (mount_lineage.go) on 2026-11-19, unchanged:
// the same paths, gates, handlers and order (LAW-10: a domain's routes
// sit in its own package, so the platform's assembly root wires domains
// instead of holding them).
//
// What the lineage reads of other domains is lent to it: Gitea access and
// the release records (delivery's; server keeps that glue), and the gates.
package assembly

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/lineage"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadtitles"
)

// New builds the lineage handler as server.New did: Gitea access and the
// release records come from the caller, the thread titles from titleStore.
func New(access lineage.AccessFunc, releases lineage.ReleasesFunc, titleStore *threadtitles.Store) *lineage.Handler {
	return lineage.NewHandler(lineage.NewService(access).WithReleases(releases)).WithTitles(func(ctx context.Context) (lineage.ThreadTitles, error) {
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

// Gates are what the platform lends the lineage's routes: the operator
// role's gate and the reporter role's gate.
type Gates struct {
	Operator func(http.Handler) http.Handler
	Reporter func(http.Handler) http.Handler
}

// Mount registers the lineage domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func Mount(r chi.Router, h *lineage.Handler, g Gates) {
	r.Get("/lineage", h.HandleGet)
	r.Get("/lineage/branches", h.HandleBranches)
	r.Group(func(r chi.Router) {
		r.Use(g.Operator)
		r.Put("/lineage/thread-title", h.HandleSetTitle)
	})
	r.Group(func(r chi.Router) {
		r.Use(g.Reporter)
		r.Put("/lineage/transcript-title", h.HandleReportTitle)
	})
}
