package agentrtapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// getPlanTimeline is GET /plan-timeline: a round's steps with their events
// and cards in one read, or only step node of it when node is given, for
// the Ops Work page.
func (a *API) getPlanTimeline(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		writeError(w, 400, "bad_request", "root is required")
		return
	}
	out, err := store.ReadRoundTimeline(r.Context(), a.DB, root, r.URL.Query().Get("node"))
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, 404, "not_found", "plan not found")
		case errors.Is(err, store.ErrInvalid):
			a.fail(w, err)
		default:
			a.logf("plan timeline: %v", err)
			writeError(w, 503, "unavailable", "plan timeline unavailable")
		}
		return
	}
	writeJSON(w, 200, out)
}

// getDelivery is GET /delivery: the delivery stages of one change, named by
// exactly one of commit or task. The PROD stages come from ReleaseCycles;
// unwired, or failing, they are unknown and the warnings say why.
func (a *API) getDelivery(w http.ResponseWriter, r *http.Request) {
	q := store.DeliveryQuery{Commit: r.URL.Query().Get("commit"), Task: r.URL.Query().Get("task")}
	if (q.Commit == "") == (q.Task == "") {
		writeError(w, 400, "bad_request", "name exactly one of commit or task")
		return
	}
	var cycles []store.ReleaseCycle
	var cyclesErr error
	if a.ReleaseCycles != nil {
		cycles, cyclesErr = a.ReleaseCycles(r.Context())
	}
	out, err := store.ReadDelivery(r.Context(), a.DB, q, cycles, cyclesErr)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, 404, "not_found", "change not found")
		case errors.Is(err, store.ErrInvalid):
			a.fail(w, err)
		default:
			a.logf("delivery: %v", err)
			writeError(w, 503, "unavailable", "delivery stages unavailable")
		}
		return
	}
	writeJSON(w, 200, out)
}
