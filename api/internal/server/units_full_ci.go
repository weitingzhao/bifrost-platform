package server

import (
	"context"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/evidence"
	"github.com/weitingzhao/bifrost-platform/api/internal/cirunview"
)

// GET /api/v1/units/full-ci is what the daily full CI recorded
// (agentrt/fullci): its last notes on the controller's stream, newest
// first, each with when, its text and its full_ci body: the runs a day
// started, or a red run with the tasks that failed and the landed commits
// whose own CI run left a module out. Read-only; viewer and above, as
// /units/config.

// unitFullCINotes is how many notes the route answers with.
const unitFullCINotes = 20

func (s *Server) handleUnitFullCI(w http.ResponseWriter, r *http.Request) {
	if s.agentRuntime == nil || s.agentRuntime.api == nil || s.agentRuntime.api.DB == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the evidence ledger is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	notes, err := evidence.FullCINotes(ctx, s.agentRuntime.api.DB, unitFullCINotes)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, unitFullCIAnswer(notes))
}

// unitFullCIAnswer is the route's body for the notes read.
func unitFullCIAnswer(notes []evidence.FullCINote) map[string]any {
	return cirunview.FullCIAnswer(notes)
}
