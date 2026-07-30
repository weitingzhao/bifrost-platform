package marketdata

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

type DeploymentInfo struct {
	Namespace string             `json:"namespace"`
	Name      string             `json:"name"`
	Ready     string             `json:"ready"`
	Reach     probe.Reachability `json:"reachability"`
	Detail    string             `json:"detail,omitempty"`
}

type WorkerInfo struct {
	Pool       string  `json:"pool"`
	Status     string  `json:"status,omitempty"`
	JobsDone   int     `json:"jobs_done"`
	JobsFailed int     `json:"jobs_failed"`
	UptimeSec  float64 `json:"uptime_sec,omitempty"`
	LastClaim  string  `json:"last_claim_at,omitempty"`
}

type StatusResponse struct {
	Reachable    bool               `json:"reachable"`
	Reachability probe.Reachability `json:"reachability"`
	Summary      string             `json:"summary"`
	Deployments  []DeploymentInfo   `json:"deployments"`
	Workers      []WorkerInfo       `json:"workers,omitempty"`
	HealthReach  probe.Reachability `json:"health_reachability"`
	Autonomy     string             `json:"autonomy"`
	Error        string             `json:"error,omitempty"`
	Hint         string             `json:"hint,omitempty"`
	GeneratedAt  time.Time          `json:"generated_at"`
}
