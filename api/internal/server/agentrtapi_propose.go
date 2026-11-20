package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
)

// sessionHeader names the calling thread, as on the questions routes.
const sessionHeader = "X-Bifrost-Session"

// writer is who a proposal is recorded as (v5 §5.1.2 actor; the design names
// seats and threads as proposers but not how a token becomes one, so the
// runtime fixes it here): the admin token is the Owner's, so it writes as
// owner; a token named seat:<x> writes as that seat (it must be registered);
// any other token writes as the thread its X-Bifrost-Session header names,
// thread:<session>, registered on its first write. The session is what the
// thread says it is: it attributes the write, it grants nothing (the role
// does). A collector token (collector:<host>, R5:5.5-12 §1.2) writes as its
// host's collector, thread:collector@<host>, whatever session it names: no
// header reaches that name, and its first write holds it to the collector's
// token (TD-370).
func writer(r *http.Request) (actor.Writer, error) {
	p := actuation.PrincipalFromContext(r.Context())
	w := actor.Writer{Token: p.Name}
	var err error
	switch {
	case p.Role == actuation.RoleIntake:
		// Defence only: an intake token is in no route's ability but the
		// intake routes', which write as intakeWriter says (§5.2, R2-A1).
		return w, fmt.Errorf("%w: an intake token writes only on the intake routes", actor.ErrNotYours)
	case p.Role == actuation.RoleAdmin:
		w.Actor = "owner"
	case actuation.IsCollector(p):
		collector, ok := actuation.CollectorActor(p)
		host, _ := actuation.CollectorHost(p)
		if !ok {
			// Defence only: LoadAuth admits a name in the namespace only as
			// a reporter named collector:<host>.
			return w, fmt.Errorf("%w: a collector token is a reporter named %s<host>", actor.ErrNotYours, actuation.CollectorPrefix)
		}
		w.Actor, w.Host = collector, host
	case strings.HasPrefix(p.Name, "seat:"):
		w.Actor = p.Name
	default:
		w.Actor, err = actor.ThreadActor(r.Header.Get(sessionHeader))
	}
	return w, err
}

// writerOf is who a write is recorded as (writer), for a route outside
// /agentrt that writes the ledger as its caller (2026-11-17: the known
// regressions on main, bus/regressions).
func writerOf(r *http.Request) (actor.Writer, error) { return writer(r) }
