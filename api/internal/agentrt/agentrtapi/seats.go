package agentrtapi

import (
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/coord"
)

// SeatStatusWindow is how far back GET /seats looks for a seat's status.
const SeatStatusWindow = 30 * 24 * time.Hour

// seatStatuses is every seat's latest status message of the last 30 days,
// newest first (GET /seats). A seat writes its status with the existing
// message route (POST /messages, a "status" envelope to coord.StatusBoard);
// one that does not parse is listed with its problem.
func (a *API) seatStatuses(w http.ResponseWriter, r *http.Request) {
	recs, err := coord.SeatStatuses(r.Context(), a.DB, time.Now().Add(-SeatStatusWindow))
	if err != nil {
		a.fail(w, err)
		return
	}
	if recs == nil {
		recs = []coord.SeatStatusRecord{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"seats": recs, "board": coord.StatusBoard})
}
