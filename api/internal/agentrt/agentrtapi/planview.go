package agentrtapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (a *API) getPlanView(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		writeError(w, 400, "bad_request", "root is required")
		return
	}
	out, err := store.PlanViewPage(r.Context(), a.DB, root, r.URL.Query().Get("recent_cursor"))
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, 404, "not_found", "plan not found")
		case errors.Is(err, store.ErrInvalid):
			a.fail(w, err)
		default:
			a.logf("plan view: %v", err)
			writeError(w, 503, "unavailable", "plan snapshot unavailable")
		}
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) registerExternalWork(w http.ResponseWriter, r *http.Request) {
	var in store.ExternalRegistration
	if !decodeView(w, r, &in) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.RegisterExternalWork(r.Context(), a.DB, who, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) postExternalEvent(w http.ResponseWriter, r *http.Request) {
	var in store.ExternalEvent
	if !decodeView(w, r, &in) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.PostExternalEvent(r.Context(), a.DB, who, chi.URLParam(r, "id"), in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (a *API) getExternalWork(w http.ResponseWriter, r *http.Request) {
	out, err := store.ReadExternalWork(r.Context(), a.DB, chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func decodeView(w http.ResponseWriter, r *http.Request, out any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err == nil {
		_, err = canon.Parse(raw)
	}
	if err == nil {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(out)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body: "+err.Error())
		return false
	}
	return true
}
