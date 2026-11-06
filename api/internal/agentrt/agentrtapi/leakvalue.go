package agentrtapi

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// getLeakValue answers GET /leak-value?weeks=1..12 (default 8): the errors
// that reached STG or PROD by place, per ISO week, and the places marked for
// a check to be added back (store/leakvalue.go).
func (a *API) getLeakValue(w http.ResponseWriter, r *http.Request) {
	weeks := store.LeakValueWeeksDefault
	if s := r.URL.Query().Get("weeks"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > store.LeakValueWeeksMax {
			writeError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("weeks is a whole number from 1 to %d", store.LeakValueWeeksMax))
			return
		}
		weeks = n
	}
	var at time.Time
	if a.Now != nil {
		at = a.Now()
	}
	out, err := store.LeakValue(r.Context(), a.DB, at, weeks)
	if err != nil {
		// Unknown, never an old copy or half of one, as /progress.
		a.logf("agentrt leak value: %v", err)
		writeError(w, http.StatusServiceUnavailable, "unavailable", "the leak value cannot be read now")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
