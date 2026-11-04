package agentrtapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
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
	// The explanation of steps in progress (R5:5.5-19) only when asked:
	// without explain=1 the response is as it was.
	if r.URL.Query().Get("explain") == "1" {
		out["explain"] = a.explainCache().Get(root)
	}
	writeJSON(w, 200, out)
}

// explainCache is the process's one explanation cache.
func (a *API) explainCache() *store.ExplainCache {
	a.explainInit.Do(func() {
		if a.Explain == nil {
			a.Explain = &store.ExplainCache{DB: a.DB, Logf: a.Logf}
		}
	})
	return a.Explain
}
func (a *API) registerExternalWork(w http.ResponseWriter, r *http.Request) {
	var in store.ExternalRegistration
	raw, ok := decodeViewRaw(w, r, &in)
	if !ok || !identityOnlyFromCollector(w, r, raw, registrationIdentity) {
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
	raw, ok := decodeViewRaw(w, r, &in)
	if !ok || !identityOnlyFromCollector(w, r, raw, eventIdentity) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	id := chi.URLParam(r, "id")
	// A collector posts only to its own work, and learns nothing of anyone
	// else's (R5:5.5-12 §1.4): work it did not register and work that does
	// not exist are refused alike, before the event is looked at. The store
	// checks the registrant again in its own transaction.
	if actuation.IsCollector(actuation.PrincipalFromContext(r.Context())) {
		if _, err := store.ReadOwnExternalWork(r.Context(), a.DB, id, who.Actor); err != nil {
			a.fail(w, err)
			return
		}
	}
	out, err := store.PostExternalEvent(r.Context(), a.DB, who, id, in)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// getExternalWork is GET /external-work/{id}. A collector reads only the
// work it registered (R5:5.5-12 §1.3 记录级归属): anything else, whether
// another registrant's or none at all, is 403.
func (a *API) getExternalWork(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if p := actuation.PrincipalFromContext(r.Context()); actuation.IsCollector(p) {
		who, err := writer(r)
		if err != nil {
			a.fail(w, err)
			return
		}
		out, err := store.ReadOwnExternalWork(r.Context(), a.DB, id, who.Actor)
		if err != nil {
			a.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	out, err := store.ReadExternalWork(r.Context(), a.DB, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// The fields of a collector's session identity (R5:5.5-12 §1.4), in a
// registration and in an event.
var (
	registrationIdentity = []string{"vendor", "session", "epoch", "sidebar"}
	eventIdentity        = []string{"vendor", "session", "epoch"}
)

// identityOnlyFromCollector refuses a body that names a session identity
// field, in any letter case encoding/json reads as it, null included, from
// a token that is no collector: 400, as such a body was before the fields
// existed, so every other registrant's contract is as it was.
func identityOnlyFromCollector(w http.ResponseWriter, r *http.Request, raw []byte, fields []string) bool {
	if actuation.IsCollector(actuation.PrincipalFromContext(r.Context())) {
		return true
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return true // decodeViewRaw read it as an object already
	}
	for key := range keys {
		for _, f := range fields {
			if strings.EqualFold(key, f) {
				writeError(w, http.StatusBadRequest, "bad_request", "body: "+f+" is a collector's field")
				return false
			}
		}
	}
	return true
}

func decodeView(w http.ResponseWriter, r *http.Request, out any) bool {
	_, ok := decodeViewRaw(w, r, out)
	return ok
}

// decodeViewRaw is decodeView, giving the body it read too.
func decodeViewRaw(w http.ResponseWriter, r *http.Request, out any) ([]byte, bool) {
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
		return nil, false
	}
	return raw, true
}
