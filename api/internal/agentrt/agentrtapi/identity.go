package agentrtapi

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// actorOf is who a write is recorded as: an agentd token is its host;
// anyone else as writer says.
func actorOf(r *http.Request) (actor.Writer, error) {
	p := actuation.PrincipalFromContext(r.Context())
	if host, ok := actuation.AgentdHost(p); ok {
		return actor.Writer{Actor: "host:" + host, Token: p.Name}, nil
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
	if host, ok := actuation.CollectorHost(p); ok {
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
		err := eventlog.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
			var err error
			registered, err = actor.CheckWriter(r.Context(), tx, who)
			return err
		})
		switch {
		case errors.Is(err, eventlog.ErrInvalid), errors.Is(err, actor.ErrNotYours):
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

// collectorAbilities are all a collector may do (R5:5.5-12 §1.3; the route
// gate holds it to them): register its external work and post its events,
// upload their descriptions; read its own work and descriptions; send its
// host's thread and host heartbeats and read that host's threads.
var collectorAbilities = []string{"external_work", "read_own", "heartbeat"}

func abilitiesOf(p actuation.Principal) []string {
	if actuation.IsCollector(p) {
		return slices.Clone(collectorAbilities)
	}
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

// document answers the one JSON document a query returns.
func (a *API) document(w http.ResponseWriter, r *http.Request, q string, args ...any) {
	var doc []byte
	err := eventlog.InTx(r.Context(), a.DB, func(tx pgx.Tx) error { return tx.QueryRow(r.Context(), q, args...).Scan(&doc) })
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
