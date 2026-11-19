package instruments

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Route answers GET /api/v1/governance/instruments, for the Crew page. Service
// is nil on a platform with no access to Gitea.
type Route struct {
	Service func() *Service
}

func (rt Route) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	svc := rt.Service()
	if svc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "gitea access is not available on this platform"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	report, err := svc.Read(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "the instruments could not be read in time"})
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
