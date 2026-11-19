package topology

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Route is the topology of one environment (the first configured, else prod).
type Route struct {
	Cfg    *config.Config
	Matrix func(ctx context.Context, env config.Environment) probe.MatrixResponse
	Live   func(context.Context) Live
}

func (rt Route) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rt.Cfg.Topology == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "topology not loaded",
		})
		return
	}

	envID := r.URL.Query().Get("env")
	if envID == "" {
		envID = "prod"
		if len(rt.Cfg.Environments) > 0 {
			envID = rt.Cfg.Environments[0].ID
		}
	}

	env, ok := rt.Cfg.GetEnvironment(envID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "unknown environment: " + envID,
		})
		return
	}

	writeJSON(w, http.StatusOK, Build(rt.Cfg.Topology, *env, rt.Matrix(r.Context(), *env), rt.Live(r.Context())))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
