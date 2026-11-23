package harnesssession

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
)

// IssueForAdmin relies on the platform's existing Admin route middleware.
func IssueForAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	r.Body = http.MaxBytesReader(w, r.Body, 256)
	var request struct {
		App string `json:"app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !Supported(request.App) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "unknown harness"})
		return
	}
	key, err := LoadSigner(os.Getenv("HARNESS_GRANT_SIGNER_FILE"))
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "harness sessions are unavailable"})
		return
	}
	grant, err := issue(key, request.App, time.Now())
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "harness sessions are unavailable"})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"grant": grant})
}
