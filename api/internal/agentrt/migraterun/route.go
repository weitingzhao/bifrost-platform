package migraterun

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/gitread"
)

// Route is POST /api/v1/agentrt/migrations/run {commit} (operator, tier C: the
// guard lets it through only on the signed policy or from the approval
// executor). It answers when the Job has finished.
type Route struct {
	Runner                   func() (*Runner, error)
	IsUncertain, IsTransient func(error) bool
}

func (rt Route) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Commit string `json:"commit"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "the body is {commit}"})
		return
	}
	commit := strings.TrimSpace(body.Commit)
	if !gitread.CommitID.MatchString(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit is a 40-character lowercase SHA"})
		return
	}
	m, err := rt.Runner()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	out, err := m.Run(r.Context(), commit)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, out)
	case rt.IsUncertain(err):
		writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": err.Error()})
	case rt.IsTransient(err):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
