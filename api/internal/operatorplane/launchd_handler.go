package operatorplane

import (
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/launchd"
)

// HandleLaunchd lists this machine's com.bifrost.* launchd agents.
// Viewer token required. It does not restart anything.
func (p *Plane) HandleLaunchd(w http.ResponseWriter, _ *http.Request) {
	services, err := launchd.List()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if services == nil {
		services = []launchd.Service{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"services": services})
}
