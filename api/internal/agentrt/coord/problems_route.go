package coord

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/problemloop"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/registry"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/relay"
)

// problemWindow is how far back the problem loop reads.
const problemWindow = 7 * 24 * time.Hour

// ProblemsRoute answers GET /api/v1/bus/problems: the letters of the rows that
// ask for a verdict, and the verdicts given (LAW-8), read from the
// messages when asked. DB is nil while the agent runtime's database is not
// connected.
type ProblemsRoute struct {
	DB func() eventlog.DB
}

func (rt ProblemsRoute) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	db := rt.DB()
	if db == nil {
		writeProblems(w, http.StatusServiceUnavailable, map[string]string{"error": "the agent runtime database is not connected"})
		return
	}
	subs, err := registry.LoadSubscriptions()
	if err != nil {
		writeProblems(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	kinds := map[string]string{}
	for _, row := range subs[relay.Tenant].Rows {
		if k := row.Params[problemloop.ParamAskVerdict]; problemloop.KnownKind(k) {
			kinds[row.ID] = k
		}
	}
	out, err := ProblemLoop(r.Context(), db, time.Now().Add(-problemWindow), kinds)
	if err != nil {
		writeProblems(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeProblems(w, http.StatusOK, out)
}

func writeProblems(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
