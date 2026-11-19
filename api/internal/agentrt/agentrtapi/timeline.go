package agentrtapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/deliveryview"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
	"github.com/weitingzhao/bifrost-platform/api/internal/releasepolicy"
)

// getDelivery is GET /delivery: the delivery stages of one commit an external
// work handed in, from its recorded pipeline runs and the current PROD
// release.
func (a *API) getDelivery(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "configuration" {
		if r.URL.Query().Get("commit") != "" || r.URL.Query().Get("task") != "" {
			writeError(w, 400, "bad_request", "configuration is a configuration snapshot; omit commit and task")
			return
		}
		if a.ConfigurationBoundaries == nil {
			writeError(w, 503, "unavailable", "configuration reader is not connected")
			return
		}
		requests, readErr := deliveryreadings.ConfigurationRequestsReadOnly(r.Context(), a.DB, listLimit)
		out, err := a.ConfigurationBoundaries(r.Context(), r.URL.Query().Get("key"), r.URL.Query().Get("value"), requests)
		if err != nil {
			writeError(w, 422, "configuration_unavailable", err.Error())
			return
		}
		if readErr != nil {
			out["requests_error"] = "Configuration request history unavailable"
		}
		out["request_history_limit"] = listLimit
		writeJSON(w, 200, out)
		return
	}
	if r.URL.Query().Get("view") == "boundaries" {
		if r.URL.Query().Get("commit") != "" || r.URL.Query().Get("task") != "" {
			writeError(w, 400, "bad_request", "boundaries is a policy snapshot; omit commit and task")
			return
		}
		out, err := deliveryview.ReadBoundaries(r.Context(), a.DB, boundarySource, a.DiskFloor)
		if err != nil {
			a.fail(w, err)
			return
		}
		writeJSON(w, 200, out)
		return
	}
	if r.URL.Query().Get("task") != "" {
		writeError(w, 400, "bad_request", "delivery by task ended with the dispatch lane; name a commit an external work handed in")
		return
	}
	q := deliveryreadings.DeliveryQuery{Commit: r.URL.Query().Get("commit")}
	if q.Commit == "" {
		writeError(w, 400, "bad_request", "commit is required")
		return
	}
	if a.ProductionDelivery != nil {
		observed := a.ProductionDelivery(r.Context())
		q.Production = &observed
	}
	out, err := deliveryreadings.ReadDelivery(r.Context(), a.DB, q)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, 404, "not_found", "change not found")
		case errors.Is(err, eventlog.ErrInvalid):
			a.fail(w, err)
		default:
			a.logf("delivery: %v", err)
			writeError(w, 503, "unavailable", "delivery stages unavailable")
		}
		return
	}
	writeJSON(w, 200, out)
}

// BoundaryRuleFrom projects one stored rule pair for the boundaries view; the
// caller has already selected the versions (no authority is established here).
func BoundaryRuleFrom(id string, current, latest *store.StoredRule, now time.Time) deliveryview.BoundaryRule {
	var c, l *deliveryview.BoundaryVersion
	if current != nil {
		c = &deliveryview.BoundaryVersion{Document: current.Doc(), Version: current.Version, Effective: current.Effective, Expires: current.Expires}
	}
	if latest != nil {
		l = &deliveryview.BoundaryVersion{Document: latest.Doc(), DraftDocument: latest.Doc(), Version: latest.Version, Effective: latest.Effective, Expires: latest.Expires}
	}
	defaults := map[string]int{}
	for _, key := range rules.TuningKeys() {
		defaults[key.Name] = key.Default
	}
	return deliveryview.ProjectBoundaryRule(id, c, l, now, rules.KindTuning, rules.Loosen, rules.Initial, defaults)
}

// boundarySource binds the boundaries view to the rule store, the rule
// registry, the landing waits and the release gate's cap. It calls the same
// CurrentRule parser as the consumers.
var boundarySource = func() deliveryview.BoundarySource {
	keys := []deliveryview.TuningKey{}
	for _, k := range rules.TuningKeys() {
		keys = append(keys, deliveryview.TuningKey{Name: k.Name, Default: k.Default, Min: k.Min, Ceiling: k.Ceiling, Unit: k.Unit, ReadBy: k.ReadBy})
	}
	return deliveryview.BoundarySource{
		RuleIDs: store.RuleIDs,
		Retired: rules.Retired,
		Rule: func(ctx context.Context, tx pgx.Tx, id string, now time.Time) (deliveryview.BoundaryRule, map[string]int, error) {
			current, err := store.CurrentRule(ctx, tx, id, now)
			if err != nil {
				return deliveryview.BoundaryRule{}, nil, err
			}
			latest, err := store.LatestRule(ctx, tx, id)
			if err != nil {
				return deliveryview.BoundaryRule{}, nil, err
			}
			var values map[string]int
			if current != nil && current.Tuning != nil {
				values = current.Tuning.Values
				if values == nil {
					values = map[string]int{}
				}
			}
			return BoundaryRuleFrom(id, current, latest, now), values, nil
		},
		Tuning:            keys,
		WaitBoundaries:    landing.WaitBoundaries(),
		ComparisonFileCap: releasepolicy.ComparisonFileCap(),
	}
}()
