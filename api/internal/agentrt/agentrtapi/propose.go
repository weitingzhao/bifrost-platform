package agentrtapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// SessionHeader names the calling thread, as on the questions routes.
const SessionHeader = "X-Bifrost-Session"

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
func writer(r *http.Request) (store.Writer, error) {
	p := actuation.PrincipalFromContext(r.Context())
	w := store.Writer{Token: p.Name}
	var err error
	switch {
	case p.Role == actuation.RoleIntake:
		// Defence only: an intake token is in no route's ability but the
		// intake routes', which write as intakeWriter says (§5.2, R2-A1).
		return w, fmt.Errorf("%w: an intake token writes only on the intake routes", store.ErrNotYours)
	case p.Role == actuation.RoleAdmin:
		w.Actor = "owner"
	case actuation.IsCollector(p):
		actor, ok := actuation.CollectorActor(p)
		host, _ := actuation.CollectorHost(p)
		if !ok {
			// Defence only: LoadAuth admits a name in the namespace only as
			// a reporter named collector:<host>.
			return w, fmt.Errorf("%w: a collector token is a reporter named %s<host>", store.ErrNotYours, actuation.CollectorPrefix)
		}
		w.Actor, w.Host = actor, host
	case strings.HasPrefix(p.Name, "seat:"):
		w.Actor = p.Name
	default:
		w.Actor, err = store.ThreadActor(r.Header.Get(SessionHeader))
	}
	return w, err
}

type budgetBody struct {
	MaxAttempts  int `json:"max_attempts"`
	MaxWallHours int `json:"max_wall_hours"`
}

// getMission is the mission row with its tasks counted by state.
func (a *API) getMission(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(m) || jsonb_build_object('task_counts', COALESCE((
	    SELECT jsonb_object_agg(state, n) FROM (SELECT state, count(*) AS n FROM agentrt.task
	      WHERE mission_id = m.mission_id GROUP BY state) s), '{}'::jsonb))
	  FROM agentrt.mission m WHERE mission_id = $1`, chi.URLParam(r, "id"))
}

// taskStates are the states a task list may filter by.
var taskStates = []string{"proposed", "awaiting_owner", "ready", "leased", "running", "submitted", "verifying",
	"accepted", "rework", "rejected", "blocked", "paused", "cancelled"}

// listLimit is the most rows a list answers.
const listLimit = 500

// listTasks is GET /tasks?mission=&state=: tasks oldest first, at most
// listLimit, and whether there were more (truncated).
func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	mission, state := r.URL.Query().Get("mission"), r.URL.Query().Get("state")
	if state != "" && !slices.Contains(taskStates, state) {
		writeError(w, http.StatusBadRequest, "bad_request", "state is one of "+strings.Join(taskStates, ", "))
		return
	}
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT jsonb_build_object(
		    'tasks', COALESCE(jsonb_agg(to_jsonb(t) - 'n' ORDER BY t.created_at, t.task_id) FILTER (WHERE t.n <= $3), '[]'::jsonb),
		    'truncated', COALESCE(max(t.n) > $3, false))
		  FROM (SELECT x.*, row_number() OVER (ORDER BY x.created_at, x.task_id) AS n FROM agentrt.task x
		    WHERE ($1 = '' OR x.mission_id = $1) AND ($2 = '' OR x.state = $2) ORDER BY x.created_at, x.task_id LIMIT $3 + 1) t`,
			mission, state, listLimit).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
