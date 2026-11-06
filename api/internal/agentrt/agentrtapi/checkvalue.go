package agentrtapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// getCheckValue is GET /check-value?weeks=: each check and retry, per ISO
// week in UTC, what it ran, caught and cost, the last weeks full weeks (1 to
// 12, default 8) and the current one, and the rows marked for deletion
// (store/checkvalue.go). Read-only.
func (a *API) getCheckValue(w http.ResponseWriter, r *http.Request) {
	weeks := store.CheckValueWeeksDefault
	if s := r.URL.Query().Get("weeks"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > store.CheckValueWeeksMax {
			writeError(w, 400, "bad_request", fmt.Sprintf("weeks is a whole number from 1 to %d", store.CheckValueWeeksMax))
			return
		}
		weeks = n
	}
	out, err := store.CheckValue(r.Context(), a.DB, weeks)
	if err != nil {
		a.logf("check value: %v", err)
		writeError(w, 503, "unavailable", "check value unavailable")
		return
	}
	writeJSON(w, 200, out)
}
