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
	NextRunAt  string  `json:"next_run_at,omitempty"`
}

// FreshnessInfo is one row from ops_jobs.ingest_freshness.
type FreshnessInfo struct {
	Dimension   string  `json:"dimension"`
	LastRunAt   string  `json:"last_run_at,omitempty"`
	RowsWritten int     `json:"rows_written"`
	Status      string  `json:"status,omitempty"`
	AgeHours    float64 `json:"age_hours"`
	Verdict     string  `json:"verdict"` // ok | stale | unknown
}

// ReadinessRollup is a Plugin-native quality KPI from
// /market/readiness/snapshot-coverage + /market/readiness/vendor-gap.
// Null/omitted when the Plugin probe fails — Console hides the KPI.
type ReadinessRollup struct {
	Universe       int    `json:"universe"`
	SnapshotRows   int    `json:"snapshot_rows"`
	SnapshotCovered int   `json:"snapshot_covered"`
	VendorGapCount int    `json:"vendor_gap_count"`
	AsOf           string `json:"as_of"`
	Source         string `json:"source"` // "plugin"
}

type StatusResponse struct {
	Reachable        bool               `json:"reachable"`
	Reachability     probe.Reachability `json:"reachability"`
	Summary          string             `json:"summary"`
	Deployments      []DeploymentInfo   `json:"deployments"`
	Workers          []WorkerInfo       `json:"workers,omitempty"`
	HealthReach      probe.Reachability `json:"health_reachability"`
	Freshness        []FreshnessInfo    `json:"freshness,omitempty"`
	FreshnessReach   probe.Reachability `json:"freshness_reachability"`
	ReadinessRollup  *ReadinessRollup   `json:"readiness_rollup,omitempty"`
	Autonomy         string             `json:"autonomy"`
	Error            string             `json:"error,omitempty"`
	Hint             string             `json:"hint,omitempty"`
	GeneratedAt      time.Time          `json:"generated_at"`
}
