package cirunview

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
)

// fullCINotes is how many notes the route answers with.
const fullCINotes = 20

// FullCIRoute is GET /api/v1/units/full-ci: what the daily full CI recorded
// (agentrt/fullci), its last notes on the controller's stream, newest first,
// each with when, its text and its full_ci body: the runs a day started, or a
// red run with the tasks that failed and the landed commits whose own CI run
// left a module out. Read-only; viewer and above, as /units/config.
type FullCIRoute struct {
	// DB is the evidence ledger; nil when this platform has none.
	DB func() eventlog.DB
}

func (rt FullCIRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	db := rt.DB()
	if db == nil {
		writeRouteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	notes, err := evidence.FullCINotes(ctx, db, fullCINotes)
	if err != nil {
		writeRouteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeRouteJSON(w, http.StatusOK, FullCIAnswer(notes))
}
