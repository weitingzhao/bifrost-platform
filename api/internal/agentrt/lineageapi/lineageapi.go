// Package lineageapi is the lineage domain's HTTP for registered work: a
// registrant registers its work and posts its events, and anyone with read
// reads one (POST /agentrt/external-work, /agentrt/external-work/{id}/events,
// GET /agentrt/external-work/{id}).
//
// The handlers were agentrtapi's (externalwork.go) and are the same code
// (OWNER_LAW #28, 2026-11-19: a domain's routes sit in its own package, so
// its contract can say what it offers). agentrtapi still mounts them, behind
// the same abilities, and lends them what every /agentrt route shares (Host).
// The plan import route stays in agentrtapi: it reads a plan as well as a
// recorder log.
package lineageapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
)

// Host is what the agent runtime's HTTP layer lends the lineage routes. Each
// read is live: the platform connects the database after the routes are
// mounted.
type Host interface {
	// DB is the agent runtime's database.
	DB() eventlog.DB
	// Writer is who a request's write is recorded as.
	Writer(*http.Request) (actor.Writer, error)
	// DecodeCanonicalRaw reads a canonical JSON body into v, refusing unknown
	// fields (400), and gives the bytes it read.
	DecodeCanonicalRaw(w http.ResponseWriter, r *http.Request, v any) ([]byte, bool)
	// JSON answers v with status.
	JSON(w http.ResponseWriter, status int, v any)
	// Error answers {"error": code, "message": msg} with status.
	Error(w http.ResponseWriter, status int, code, msg string)
	// Fail answers an error of the store with its status.
	Fail(w http.ResponseWriter, err error)
}

// Routes are the lineage domain's handlers for registered work.
type Routes struct{ Host Host }

// HandleRegisterExternalWork is POST /external-work.
func (rt Routes) HandleRegisterExternalWork(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var in externalwork.ExternalRegistration
	raw, ok := h.DecodeCanonicalRaw(w, r, &in)
	if !ok || !rt.identityOnlyFromCollector(w, r, raw, registrationIdentity) {
		return
	}
	who, err := h.Writer(r)
	if err != nil {
		h.Fail(w, err)
		return
	}
	out, err := externalwork.RegisterExternalWork(r.Context(), h.DB(), who, in)
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, 200, out)
}

// HandlePostExternalEvent is POST /external-work/{id}/events.
func (rt Routes) HandlePostExternalEvent(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var in externalwork.ExternalEvent
	raw, ok := h.DecodeCanonicalRaw(w, r, &in)
	if !ok || !rt.identityOnlyFromCollector(w, r, raw, eventIdentity) {
		return
	}
	who, err := h.Writer(r)
	if err != nil {
		h.Fail(w, err)
		return
	}
	id := chi.URLParam(r, "id")
	// A collector posts only to its own work, and learns nothing of anyone
	// else's (R5:5.5-12 §1.4): work it did not register and work that does
	// not exist are refused alike, before the event is looked at. The store
	// checks the registrant again in its own transaction.
	if actuation.IsCollector(actuation.PrincipalFromContext(r.Context())) {
		if _, err := externalwork.ReadOwnExternalWork(r.Context(), h.DB(), id, who.Actor); err != nil {
			h.Fail(w, err)
			return
		}
	}
	out, err := externalwork.PostExternalEvent(r.Context(), h.DB(), who, id, in)
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, 200, out)
}

// HandleGetExternalWork is GET /external-work/{id}. A collector reads only
// the work it registered (R5:5.5-12 §1.3 记录级归属): anything else, whether
// another registrant's or none at all, is 403.
func (rt Routes) HandleGetExternalWork(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	id := chi.URLParam(r, "id")
	if p := actuation.PrincipalFromContext(r.Context()); actuation.IsCollector(p) {
		who, err := h.Writer(r)
		if err != nil {
			h.Fail(w, err)
			return
		}
		out, err := externalwork.ReadOwnExternalWork(r.Context(), h.DB(), id, who.Actor)
		if err != nil {
			h.Fail(w, err)
			return
		}
		h.JSON(w, 200, out)
		return
	}
	out, err := externalwork.ReadExternalWork(r.Context(), h.DB(), id)
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, 200, out)
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
func (rt Routes) identityOnlyFromCollector(w http.ResponseWriter, r *http.Request, raw []byte, fields []string) bool {
	if actuation.IsCollector(actuation.PrincipalFromContext(r.Context())) {
		return true
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return true // the body was read as an object already
	}
	for key := range keys {
		for _, f := range fields {
			if strings.EqualFold(key, f) {
				rt.Host.Error(w, http.StatusBadRequest, "bad_request", "body: "+f+" is a collector's field")
				return false
			}
		}
	}
	return true
}
