package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/safego"
)

// Routes are the connectivity matrix and mission verification reads.
type Routes struct {
	Cfg    *config.Config
	Prober *Prober
	// Snapshot is the datastores as the cluster sees them, nil without one.
	Snapshot func(context.Context) *DatastoreSnapshot
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// HandleEnvironments lists the environments, id and label.
func (rt Routes) HandleEnvironments(w http.ResponseWriter, _ *http.Request) {
	envs := make([]map[string]string, 0, len(rt.Cfg.Environments))
	for _, e := range rt.Cfg.Environments {
		envs = append(envs, map[string]string{
			"id":    e.ID,
			"label": e.Label,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": envs})
}

// HandleMatrix is the connectivity matrix of one environment or of all.
func (rt Routes) HandleMatrix(w http.ResponseWriter, r *http.Request) {
	envID := r.URL.Query().Get("env")
	ctx := r.Context()
	ds := rt.Snapshot(ctx)

	if envID != "" {
		env, ok := rt.Cfg.GetEnvironment(envID)
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "unknown environment: " + envID,
			})
			return
		}
		writeJSON(w, http.StatusOK, rt.Prober.ProbeEnvironmentWithDatastore(ctx, *env, ds))
		return
	}

	results := make([]MatrixResponse, len(rt.Cfg.Environments))
	var wg sync.WaitGroup
	for i, env := range rt.Cfg.Environments {
		wg.Add(1)
		go func(idx int, e config.Environment) {
			defer safego.Recover("server.probeEnvironment")
			defer wg.Done()
			results[idx] = rt.Prober.ProbeEnvironmentWithDatastore(ctx, e, ds)
		}(i, env)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, map[string]any{"matrices": results})
}

// HandleVerifyPayload is the mission's payload verification.
func (rt Routes) HandleVerifyPayload(w http.ResponseWriter, r *http.Request) {
	matrices, dsSnap := rt.missionMatrices(r.Context())
	writeJSON(w, http.StatusOK, VerifyPayload(rt.Cfg.Environments, matrices, dsSnap))
}

// HandleVerifyMissionSnapshot is the mission's snapshot verification.
func (rt Routes) HandleVerifyMissionSnapshot(w http.ResponseWriter, r *http.Request) {
	matrices, dsSnap := rt.missionMatrices(r.Context())
	writeJSON(w, http.StatusOK, VerifyMissionSnapshot(rt.Cfg.Environments, matrices, dsSnap))
}

func (rt Routes) missionMatrices(ctx context.Context) ([]MatrixResponse, DatastoreSnapshot) {
	dsSnap := DatastoreSnapshot{}
	if snap := rt.Snapshot(ctx); snap != nil {
		dsSnap = *snap
	}
	matrices := make([]MatrixResponse, 0, len(rt.Cfg.Environments))
	for _, env := range rt.Cfg.Environments {
		matrices = append(matrices, rt.Prober.ProbeEnvironmentWithDatastore(ctx, env, &dsSnap))
	}
	return matrices, dsSnap
}
