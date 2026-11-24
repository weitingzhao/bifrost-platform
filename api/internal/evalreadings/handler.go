package evalreadings

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Handler serves GET /api/v1/readings/evaluation?days=7&name=.
//
// days is the window: the last `days` whole days in the Owner's zone that have
// ended (1 to 31, default 7). name keeps only the readings of that name. db
// and prom are read per request; nil leaves their readings unmeasured. tasks
// names a failed CI run's failed tasks; nil leaves them unrecorded.
func Handler(db func() eventlog.DB, prom func() Prom, tasks FailedTasks, now func() time.Time) http.HandlerFunc {
	if now == nil {
		now = time.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		days := 7
		if v := q.Get("days"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 31 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "days is 1 to 31"})
				return
			}
			days = n
		}
		win := ClosedWindow(now(), days)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		var d eventlog.DB
		if db != nil {
			d = db()
		}
		var p Prom
		if prom != nil {
			p = prom()
		}
		list, err := Compute(ctx, d, p, tasks, win)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if name := q.Get("name"); name != "" {
			kept := list[:0]
			for _, rd := range list {
				if rd.Name == name {
					kept = append(kept, rd)
				}
			}
			list = kept
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"window":             win,
			"definition_version": DefinitionVersion,
			"readings":           list,
			"note":               "Law 93: numbers come only from the system. unmeasured = no records; too_few = fewer samples than min_samples, no percentile given.",
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
