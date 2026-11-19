package planapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

// HandleGetPlanPace is GET /plan-pace?root=: a round's steps left, its steps done
// on each of the last days in the Owner's time zone, and at that pace how many
// days remain, with the one sentence every page and report reads
// (R5:5.5-13 slice 1). Read-only; a separate route so a Console that does not
// know it keeps reading plan-view as before.
func (rt Routes) HandleGetPlanPace(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		rt.Host.Error(w, 400, "bad_request", "root is required")
		return
	}
	out, err := planreadings.PlanPace(r.Context(), rt.Host.DB(), root, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			rt.Host.Error(w, 404, "not_found", "plan not found")
		default:
			rt.Host.Logf("plan pace: %v", err)
			rt.Host.Error(w, 503, "unavailable", "plan pace unavailable")
		}
		return
	}
	rt.Host.JSON(w, 200, out)
}
