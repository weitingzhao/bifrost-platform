package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
)

// mountCoordination registers the coordination domain's routes under /api/v1. They were lines
// of Router until 2026-11-16, in one list ordered by who may call them; each
// route keeps its path, its handler and the same gate.
func (s *Server) mountCoordination(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.auth.Require(actuation.RoleReporter))
		// A session's own turn and tool events (W-54); same token as its title.
		r.Post("/agent/threads/heartbeat", s.agentThreads.HandleBeat)
		// One machine's hook wiring and token readability (W-57).
		r.Post("/agent/hosts/heartbeat", s.agentThreads.HandleHost)
	})
	// Titles and host names: viewer and above.
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/agent/threads", s.agentThreads.HandleList)
}
