package planapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// HandleGetPlanView is GET /plan-view?root=&recent_cursor=: the plan view's
// page of a root.
func (rt Routes) HandleGetPlanView(w http.ResponseWriter, r *http.Request) {
	root := r.URL.Query().Get("root")
	if root == "" {
		rt.Host.Error(w, 400, "bad_request", "root is required")
		return
	}
	out, err := planstore.PlanViewPage(r.Context(), rt.Host.DB(), root, r.URL.Query().Get("recent_cursor"))
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			rt.Host.Error(w, 404, "not_found", "plan not found")
		case errors.Is(err, eventlog.ErrInvalid):
			rt.Host.Fail(w, err)
		default:
			rt.Host.Logf("plan view: %v", err)
			rt.Host.Error(w, 503, "unavailable", "plan snapshot unavailable")
		}
		return
	}
	rt.sessionCoverage(r.Context(), out)
	rt.Host.JSON(w, 200, out)
}

// sessionCoverage adds coverage.sessions (R5:5.5-12 §3.2): the agent
// sessions of every host, not of this root alone, as its scope says. A
// platform without the thread list, or one that cannot read it, leaves it out
// and says so in the warnings; the rest of the view is as it was.
func (rt Routes) sessionCoverage(ctx context.Context, out map[string]any) {
	planreadings.AppendSessionCoverage(ctx, out, rt.Host.Sessions(), rt.Host.Logf)
}
