package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
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
