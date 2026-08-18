package flexquery

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
	Pool       string `json:"pool"`
	Status     string `json:"status,omitempty"`
	JobsDone   int    `json:"jobs_done"`
	JobsFailed int    `json:"jobs_failed"`
	LastClaim  string `json:"last_claim_at,omitempty"`
}

type FreshnessInfo struct {
	Dimension   string  `json:"dimension"`
	LastRunAt   string  `json:"last_run_at,omitempty"`
	RowsWritten int     `json:"rows_written"`
	Status      string  `json:"status,omitempty"`
	AgeHours    float64 `json:"age_hours"`
	Verdict     string  `json:"verdict"`
}

type StatusResponse struct {
	Reachable      bool               `json:"reachable"`
	Reachability   probe.Reachability `json:"reachability"`
	Summary        string             `json:"summary"`
	Deployments    []DeploymentInfo   `json:"deployments"`
	Workers        []WorkerInfo       `json:"workers,omitempty"`
	HealthReach    probe.Reachability `json:"health_reachability"`
	Freshness      []FreshnessInfo    `json:"freshness,omitempty"`
	FreshnessReach probe.Reachability `json:"freshness_reachability"`
	Autonomy       string             `json:"autonomy"`
	Error          string             `json:"error,omitempty"`
	Hint           string             `json:"hint,omitempty"`
	GeneratedAt    time.Time          `json:"generated_at"`
}
