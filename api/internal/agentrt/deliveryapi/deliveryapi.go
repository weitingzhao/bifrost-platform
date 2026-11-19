// Package deliveryapi is the delivery domain's HTTP under /agentrt: GET
// /agentrt/delivery, the delivery stages of one commit an external work
// handed in, the boundaries snapshot (view=boundaries) and the configuration
// snapshot (view=configuration).
//
// The handler was agentrtapi's (getDelivery of timeline.go) and is the same
// code (OWNER_LAW #28, 2026-11-19: a domain's routes sit in its own package,
// so its contract can say what it offers). agentrtapi still mounts it, behind
// the same ability, and lends it what every /agentrt route shares (Host). The
// boundaries view reads the rule store, the rule registry and the landing
// waits (verification); the assembly binds them (Routes.Boundaries), so this
// package imports no verification package.
package deliveryapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Host is what the agent runtime's HTTP layer lends the delivery route. Each
// read is live: the platform connects the database and its readers after the
// routes are mounted.
type Host interface {
	// DB is the agent runtime's database.
	DB() eventlog.DB
	// ConfigurationBoundaries reads the configuration snapshot; nil means
	// not connected.
	ConfigurationBoundaries() func(context.Context, string, string, []map[string]any) (map[string]any, error)
	// DiskFloor is the historical disk boundary configuration.
	DiskFloor() int64
	// ProductionDelivery reads the current PROD release; nil means not
	// wired, and the PROD stages say so.
	ProductionDelivery() func(context.Context) deliveryview.DeliveryProduction
	// JSON answers v with status.
	JSON(w http.ResponseWriter, status int, v any)
	// Error answers {"error": code, "message": msg} with status.
	Error(w http.ResponseWriter, status int, code, msg string)
	// Fail answers an error of the store with its status.
	Fail(w http.ResponseWriter, err error)
	// Logf logs.
	Logf(format string, args ...any)
}

// Routes are the delivery domain's handlers under /agentrt.
type Routes struct {
	Host Host
	// Boundaries binds the boundaries view to the rule store, the rule
	// registry, the landing waits and the release gate's cap (bound by the
	// assembly).
	Boundaries deliveryview.BoundarySource
}

// listLimit is the most rows a list answers, as every list of the agent
// runtime's routes.
const listLimit = 500

// HandleGetDelivery is GET /delivery: the delivery stages of one commit an
// external work handed in, from its recorded pipeline runs and the current
// PROD release.
func (rt Routes) HandleGetDelivery(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	if r.URL.Query().Get("view") == "configuration" {
		if r.URL.Query().Get("commit") != "" || r.URL.Query().Get("task") != "" {
			h.Error(w, 400, "bad_request", "configuration is a configuration snapshot; omit commit and task")
			return
		}
		boundaries := h.ConfigurationBoundaries()
		if boundaries == nil {
			h.Error(w, 503, "unavailable", "configuration reader is not connected")
			return
		}
		requests, readErr := deliveryreadings.ConfigurationRequestsReadOnly(r.Context(), h.DB(), listLimit)
		out, err := boundaries(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("value"), requests)
		if err != nil {
			h.Error(w, 422, "configuration_unavailable", err.Error())
			return
		}
		if readErr != nil {
			out["requests_error"] = "Configuration request history unavailable"
		}
		out["request_history_limit"] = listLimit
		h.JSON(w, 200, out)
		return
	}
	if r.URL.Query().Get("view") == "boundaries" {
		if r.URL.Query().Get("commit") != "" || r.URL.Query().Get("task") != "" {
			h.Error(w, 400, "bad_request", "boundaries is a policy snapshot; omit commit and task")
			return
		}
		out, err := deliveryview.ReadBoundaries(r.Context(), h.DB(), rt.Boundaries, h.DiskFloor())
		if err != nil {
			h.Fail(w, err)
			return
		}
		h.JSON(w, 200, out)
		return
	}
	if r.URL.Query().Get("task") != "" {
		h.Error(w, 400, "bad_request", "delivery by task ended with the dispatch lane; name a commit an external work handed in")
		return
	}
	q := deliveryreadings.DeliveryQuery{Commit: r.URL.Query().Get("commit")}
	if q.Commit == "" {
		h.Error(w, 400, "bad_request", "commit is required")
		return
	}
	if production := h.ProductionDelivery(); production != nil {
		observed := production(r.Context())
		q.Production = &observed
	}
	out, err := deliveryreadings.ReadDelivery(r.Context(), h.DB(), q)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			h.Error(w, 404, "not_found", "change not found")
		case errors.Is(err, eventlog.ErrInvalid):
			h.Fail(w, err)
		default:
			h.Logf("delivery: %v", err)
			h.Error(w, 503, "unavailable", "delivery stages unavailable")
		}
		return
	}
	h.JSON(w, 200, out)
}
