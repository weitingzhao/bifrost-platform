// Package planapi is the plan domain's HTTP: the plan tree and a node
// (GET /agentrt/plan, /agentrt/plan/{id}), the plan view and its pace
// (GET /agentrt/plan-view, /agentrt/plan-pace), the progress projection
// (GET /agentrt/progress), and the writes a proposer or the Owner makes on
// the plan (POST /agentrt/plan/proposals, /agentrt/plan/{id}/notes,
// /agentrt/plan/{id}/signoff).
//
// The handlers were agentrtapi's (plan.go, planview.go, planpace.go) and are
// the same code (OWNER_LAW #28, 2026-11-19: a domain's routes sit in its own
// package, so its contract can say what it offers). agentrtapi still mounts
// them, behind the same abilities, and lends them what every /agentrt route
// shares (Host): who a request is recorded as, the strict body readers, the
// answers and the mapping of an error to a status. The plan import route
// stays in agentrtapi: it reads a recorder log (lineage) as well as a plan.
package planapi

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// Host is what the agent runtime's HTTP layer lends the plan routes. Each
// read is live: the platform connects the database and wires the Owner's
// card after the routes are mounted.
type Host interface {
	// DB is the agent runtime's database.
	DB() eventlog.DB
	// Ask creates the Owner's decision card for a plan change outside the
	// standing authorization.
	Ask() planstore.Ask
	// Sessions counts the retained agent sessions for the plan view's
	// coverage.sessions; nil leaves it out.
	Sessions() func(context.Context) (any, error)
	// Back is when the runtime's routes became ready (the zero time when
	// not known).
	Back() time.Time
	// Writer is who a request's write is recorded as.
	Writer(*http.Request) (actor.Writer, error)
	// Decode reads a JSON body into v, refusing unknown fields (400).
	Decode(w http.ResponseWriter, r *http.Request, v any) bool
	// DecodeCanonical is Decode that also refuses a body that is not
	// canonical JSON.
	DecodeCanonical(w http.ResponseWriter, r *http.Request, v any) bool
	// Document answers the one JSON document a query returns.
	Document(w http.ResponseWriter, r *http.Request, q string, args ...any)
	// JSON answers v with status.
	JSON(w http.ResponseWriter, status int, v any)
	// Error answers {"error": code, "message": msg} with status.
	Error(w http.ResponseWriter, status int, code, msg string)
	// Fail answers an error of the store with its status.
	Fail(w http.ResponseWriter, err error)
	// Logf logs.
	Logf(format string, args ...any)
}

// Routes are the plan domain's handlers.
type Routes struct {
	Host Host
	// WorkSpans reads registered work for GET /plan-pace (lineage's reader,
	// bound by the assembly).
	WorkSpans planreadings.WorkSpans
}
