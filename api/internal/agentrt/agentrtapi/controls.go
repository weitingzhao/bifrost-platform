package agentrtapi

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// actorOf is who a write is recorded as: an agentd token is its host;
// anyone else as writer says.
func actorOf(r *http.Request) (store.Writer, error) {
	p := actuation.PrincipalFromContext(r.Context())
	if host, ok := actuation.AgentdHost(p); ok {
		return store.Writer{Actor: "host:" + host, Token: p.Name}, nil
	}
	return writer(r)
}

// whoami says who the caller is, who its writes are recorded as and what it
// may do (v5 §5.2: 身份、岗位、能做什么). A thread without a session header
// sees why it cannot write.
func (a *API) whoami(w http.ResponseWriter, r *http.Request) {
	p := actuation.PrincipalFromContext(r.Context())
	out := map[string]any{"name": p.Name, "role": p.Role}
	if host, ok := actuation.AgentdHost(p); ok {
		out["host_id"] = host
	}
	if who, err := actorOf(r); err != nil {
		out["actor_error"] = err.Error()
	} else {
		out["actor"] = who.Actor
		// The same check a write makes, without writing (review 176 part 2
		// B1): retired, of another kind, not registered, or a thread's
		// session another token's (TD-370) say so.
		var registered bool
		err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
			var err error
			registered, err = store.CheckWriter(r.Context(), tx, who)
			return err
		})
		switch {
		case errors.Is(err, store.ErrInvalid), errors.Is(err, store.ErrNotYours):
			out["actor_error"] = err.Error()
		case err != nil:
			a.fail(w, err)
			return
		case registered:
			out["actor_kind"] = kindName(who.Actor)
		default:
			out["actor_kind"] = "thread (registered on its first write)"
		}
	}
	out["may"] = abilitiesOf(p)
	writeJSON(w, http.StatusOK, out)
}

// kindName is the kind an actor's name says, which CheckWriter has
// confirmed for a registered actor.
func kindName(actor string) string {
	for _, k := range []string{"thread", "seat", "host", "exec"} {
		if strings.HasPrefix(actor, k+":") {
			if k == "exec" {
				return "executor"
			}
			return k
		}
	}
	return actor
}

func abilitiesOf(p actuation.Principal) []string {
	var out []string
	for _, ab := range abilities {
		if limitedReporter(p) && (ab.name == "propose" || ab.name == "upload") {
			continue
		}
		if slices.Contains(ab.roles, p.Role) {
			out = append(out, ab.name)
		}
	}
	if limitedReporter(p) {
		out = append(out, "external_work")
	}
	return out
}

type controlBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Scope          string `json:"scope"`
	ScopeRef       string `json:"scope_ref"`
	Kind           string `json:"kind"`
	Reason         string `json:"reason"`
}

// setControl is POST /controls: control_set with a new id.
func (a *API) setControl(w http.ResponseWriter, r *http.Request) {
	var b controlBody
	if !decode(w, r, &b) {
		return
	}
	a.control(w, r, b)
}

func (a *API) control(w http.ResponseWriter, r *http.Request, b controlBody) {
	who, err := actorOf(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	out, err := store.RequestControl(r.Context(), a.DB, who, b.IdempotencyKey, b.Scope, b.ScopeRef, b.Kind, b.Reason)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type drainBody struct {
	IdempotencyKey string `json:"idempotency_key"`
	Reason         string `json:"reason"`
}

// drainHost is POST /hosts/{id}/drain: a drain control on the host, so the
// host takes no new work while what it runs goes on; the Owner lifts it
// like any control.
func (a *API) drainHost(w http.ResponseWriter, r *http.Request) {
	var b drainBody
	if !decode(w, r, &b) {
		return
	}
	a.control(w, r, controlBody{IdempotencyKey: b.IdempotencyKey, Scope: "host", ScopeRef: chi.URLParam(r, "id"), Kind: "drain", Reason: b.Reason})
}

// liftControl is DELETE /controls/{id}: control_cleared by the Owner.
func (a *API) liftControl(w http.ResponseWriter, r *http.Request) {
	out, err := store.LiftControl(r.Context(), a.DB, "owner", chi.URLParam(r, "id"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// listControls is GET /controls: the controls in force, or with all=1 every
// control, newest first, at most listLimit.
func (a *API) listControls(w http.ResponseWriter, r *http.Request) {
	all := r.URL.Query().Get("all") == "1"
	a.list(w, r, `SELECT jsonb_build_object(
	    'controls', COALESCE(jsonb_agg(to_jsonb(c) - 'n' ORDER BY c.n) FILTER (WHERE c.n <= $2), '[]'::jsonb),
	    'truncated', COALESCE(max(c.n) > $2, false))
	  FROM (SELECT x.*, row_number() OVER (ORDER BY x.set_at DESC, x.control_id) AS n FROM agentrt.control x
	    WHERE $1 OR x.cleared_at IS NULL ORDER BY x.set_at DESC, x.control_id LIMIT $2 + 1) c`, all, listLimit)
}

// listHosts is GET /hosts: every host with how long since it last polled,
// whether that counts as online (v5 §5.2: online is not stored, it is read
// from last_poll_at; W-31 3.5: lost past 30 seconds, store.HostLostAfter),
// its live attempts, the controls on it, what its latest claim offered,
// whether its disk is low and whether the controller reports it offline
// (store.HostsSQL). The document is the progress projection's, at the
// statement's moment.
func (a *API) listHosts(w http.ResponseWriter, r *http.Request) {
	a.list(w, r, `SELECT jsonb_build_object('hosts', (`+store.HostsSQL("statement_timestamp()")+`))`, store.HostsArgs(a.back())...)
}

// list answers the one JSON document a query returns.
func (a *API) list(w http.ResponseWriter, r *http.Request, q string, args ...any) {
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error { return tx.QueryRow(r.Context(), q, args...).Scan(&doc) })
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
