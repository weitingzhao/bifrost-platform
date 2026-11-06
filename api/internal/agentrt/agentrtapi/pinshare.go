package agentrtapi

import (
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// getPinShare is GET /pin-share: of the implement tasks proposed in the last
// store.PinShareDays, how many are pinned to the coordinator and how many of
// those say no reason, with the one sentence Work shows (R5:5.7-8 slice 1).
// Read-only, from the tasks and their creation events.
func (a *API) getPinShare(w http.ResponseWriter, r *http.Request) {
	now := time.Now
	if a.Now != nil {
		now = a.Now
	}
	out, err := store.PinShare(r.Context(), a.DB, now())
	if err != nil {
		a.logf("pin share: %v", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "pin share unavailable")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
