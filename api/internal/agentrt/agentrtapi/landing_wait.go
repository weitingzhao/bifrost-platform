package agentrtapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (a *API) refreshLanding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Task string `json:"task"`
		Op   string `json:"op"`
	}
	if !decode(w, r, &req) {
		return
	}
	host, ok := actuation.AgentdHost(actuation.PrincipalFromContext(r.Context()))
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden", "an execution identity is required")
		return
	}
	if a.MainHead == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "main reader unavailable")
		return
	}
	wrote, err := store.RefreshLanding(r.Context(), a.DB, req.Task, req.Op, host, a.MainHead)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": req.Task, "op": req.Op, "refreshed": wrote})
}

func (a *API) getLandingWait(w http.ResponseWriter, r *http.Request) {
	status, err := store.ReadLandingWait(r.Context(), a.DB, chi.URLParam(r, "op"))
	if err != nil {
		a.fail(w, err)
		return
	}
	if status == nil {
		writeError(w, http.StatusNotFound, "not_found", "no authorization wait for this operation")
		return
	}
	if a.MainHead != nil && status.Phase == "awaiting_grant" {
		if head, err := a.MainHead(r.Context(), "bifrost-platform"); err == nil {
			status.ObserveMain(head)
		}
	}
	writeJSON(w, http.StatusOK, status)
}
