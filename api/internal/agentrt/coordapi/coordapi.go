// Package coordapi is the coordination domain's HTTP under /agentrt:
// messages between actors (POST and GET /messages, GET /messages/thread,
// POST /messages/ack), the seats' statuses (GET /seats) and the controls
// (GET and POST /controls, DELETE /controls/{id}). The handlers were
// agentrtapi's (messages.go, seats.go and the controls of controls.go) and
// are the same code (OWNER_LAW #95, 2026-11-19: a domain's routes sit in its
// own package, so its contract can say what it offers). agentrtapi still
// mounts them, behind the same abilities (stop, lift, propose, read), and
// lends them what every /agentrt route shares (Host). They call coord's
// functions as before; a package apart from coord, so what coord exports
// stays used by production code of another package.
package coordapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Host is what the agent runtime's HTTP layer lends the coordination
// routes. Each read is live: the platform connects the database and the
// bus's wake after the routes are mounted.
type Host interface {
	// DB is the agent runtime's database.
	DB() eventlog.DB
	// ClaimPoll, when not zero, replaces how often a waiting inbox read
	// re-reads the record (tests).
	ClaimPoll() time.Duration
	// MailWake registers a waiter on an address (nil: waiters only re-read
	// the record).
	MailWake() func(addr string) (woken <-chan struct{}, stop func())
	// Writer is who a request's write is recorded as.
	Writer(*http.Request) (actor.Writer, error)
	// ActorOf is Writer, except that an agentd token writes as its host.
	ActorOf(*http.Request) (actor.Writer, error)
	// Decode reads a JSON body into v, refusing unknown fields (400).
	Decode(w http.ResponseWriter, r *http.Request, v any) bool
	// Document answers the one JSON document a query returns.
	Document(w http.ResponseWriter, r *http.Request, q string, args ...any)
	// JSON answers v with status.
	JSON(w http.ResponseWriter, status int, v any)
	// Error answers {"error": code, "message": msg} with status.
	Error(w http.ResponseWriter, status int, code, msg string)
	// Fail answers an error of the store with its status.
	Fail(w http.ResponseWriter, err error)
}

// Routes are the coordination domain's handlers under /agentrt.
type Routes struct {
	Host Host
}

// listLimit is the most controls GET /controls answers, as every list of
// the agent runtime's routes.
const listLimit = 500

type controlBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Scope          string `json:"scope"`
	ScopeRef       string `json:"scope_ref"`
	Kind           string `json:"kind"`
	Reason         string `json:"reason"`
}

// HandleSetControl is POST /controls: control_set with a new id.
func (rt Routes) HandleSetControl(w http.ResponseWriter, r *http.Request) {
	var b controlBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	rt.control(w, r, b)
}

func (rt Routes) control(w http.ResponseWriter, r *http.Request, b controlBody) {
	who, err := rt.Host.ActorOf(r)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	out, err := coord.RequestControl(r.Context(), rt.Host.DB(), who, b.IdempotencyKey, b.Scope, b.ScopeRef, b.Kind, b.Reason)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, out)
}

// HandleLiftControl is DELETE /controls/{id}: control_cleared by the Owner.
func (rt Routes) HandleLiftControl(w http.ResponseWriter, r *http.Request) {
	out, err := coord.LiftControl(r.Context(), rt.Host.DB(), "owner", chi.URLParam(r, "id"))
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, out)
}

// HandleListControls is GET /controls: the controls in force, or with all=1
// every control, newest first, at most listLimit.
func (rt Routes) HandleListControls(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	rt.Host.Document(w, r, `SELECT jsonb_build_object(
	    'controls', COALESCE(jsonb_agg(to_jsonb(c) - 'n' ORDER BY c.n) FILTER (WHERE c.n <= $2), '[]'::jsonb),
	    'truncated', COALESCE(max(c.n) > $2, false))
	  FROM (SELECT x.*, row_number() OVER (ORDER BY x.set_at DESC, x.control_id) AS n FROM agentrt.control x
	    WHERE $1 OR x.cleared_at IS NULL ORDER BY x.set_at DESC, x.control_id LIMIT $2 + 1) c`, all, listLimit)
}
