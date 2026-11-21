package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/homelayout"
)

// The Owner's arrangement of the Ops home's city (package homelayout has the
// contract): anyone who reads the Console reads it; only the Owner saves it.
// The handlers live in homelayout; this mounts and gates them.

func (s *Server) homeLayout() homelayout.Handlers {
	return homelayout.Handlers{DB: s.recordDB, Who: writerOf}
}

func (s *Server) mountHomeLayout(r chi.Router) {
	r.With(s.auth.Require(actuation.RoleViewer)).Get("/home/layout", s.handleHomeLayout)
	r.With(s.auth.RequireAny(actuation.RoleAdmin)).Put("/home/layout", s.guard("save_home_layout", s.handleHomeLayoutSave))
}

func (s *Server) handleHomeLayout(w http.ResponseWriter, r *http.Request) {
	s.homeLayout().Get(w, r)
}

// handleHomeLayoutSave is named by actions_wire.go.
func (s *Server) handleHomeLayoutSave(w http.ResponseWriter, r *http.Request) {
	s.homeLayout().Save(w, r)
}
