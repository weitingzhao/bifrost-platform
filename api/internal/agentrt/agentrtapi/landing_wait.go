package agentrtapi

import (
	"errors"
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

// getLandingMaterial is a landing grant's material (C-5, §5.6 item 3): what
// the Owner is shown and the digests a request must carry, recomputed now.
// stg_release is the request's parameter (true unless "false", O-1 A).
func (a *API) getLandingMaterial(w http.ResponseWriter, r *http.Request) {
	if a.Grants == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "landing grant evidence is not read in this process")
		return
	}
	stg := r.URL.Query().Get("stg_release") != "false"
	m, err := store.ReadLandingGrantMaterial(r.Context(), a.DB, a.Grants(), chi.URLParam(r, "op"), stg)
	if err != nil {
		if materialRetry(w, err) {
			return
		}
		if errors.Is(err, store.ErrMaterialTooLarge) {
			writeError(w, http.StatusUnprocessableEntity, "material_too_large", err.Error())
			return
		}
		if errors.Is(err, store.ErrInvalid) {
			a.fail(w, err)
			return
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable", err.Error())
		return
	}
	a.writeLandingMaterial(w, r, m)
}

// Main is a live observation, never part of the reusable Git evidence.
func (a *API) writeLandingMaterial(w http.ResponseWriter, r *http.Request, m *store.LandingGrantMaterial) {
	if a.MainHead != nil {
		head, err := a.MainHead(r.Context(), m.Candidate.Repo)
		writeJSON(w, http.StatusOK, map[string]any{"material": m, "main": head, "main_known": err == nil && head != "",
			"main_is_M0": err == nil && head == m.Candidate.M0})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"material": m, "main": "", "main_known": false, "main_is_M0": false})
}

func materialRetry(w http.ResponseWriter, err error) bool {
	code := ""
	switch {
	case errors.Is(err, store.ErrMaterialPending):
		code = "material_pending"
	case errors.Is(err, store.ErrMaterialBusy):
		code = "material_busy"
	default:
		return false
	}
	w.Header().Set("Retry-After", "2")
	writeError(w, http.StatusTooManyRequests, code, err.Error())
	return true
}

// revokeLandingGrant is the Owner's revocation (F2 A, §5.7): a tightening,
// no approval. A grant already consumed is not revoked; the answer says it
// was handed to the push. An unknown outcome answers 503: the stream decides.
func (a *API) revokeLandingGrant(w http.ResponseWriter, r *http.Request) {
	out, err := store.RevokeLandingGrant(r.Context(), a.DB, chi.URLParam(r, "grant_id"))
	if errors.Is(err, store.ErrGrantUncertain) {
		writeError(w, http.StatusServiceUnavailable, "unknown", "the result is unknown; refresh the material page, which reads the stream: "+err.Error())
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
