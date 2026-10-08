package releasepolicy

import (
	"encoding/json"
	"net/http"
)

// HandleStatus serves GET /api/v1/release-policy.
func (e *Engine) HandleStatus(w http.ResponseWriter, r *http.Request) {
	st := e.Status(r.Context())
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(st)
}
