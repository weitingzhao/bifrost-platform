package approvalreadings

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvals"
)

// Route answers the approval readings from the approval records, in Zone.
type Route struct {
	Zone string
	// Records are the approval records; nil when this platform keeps none.
	Records func() ([]approvals.Approval, error)
}

func (rt Route) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if rt.Records == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "this platform keeps no approval records"})
		return
	}
	loc, err := time.LoadLocation(rt.Zone)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items, err := rt.Records()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "the approval records could not be read: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, Compute(items, loc, time.Now()))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
