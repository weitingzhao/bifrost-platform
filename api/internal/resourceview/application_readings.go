package resourceview

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The shapes the cluster package hands to whoever reads the tenant's
// applications and data freshness, and gets back. They are the base's: the
// cluster package gathers the observations and answers the routes, the
// tenant domain (tenant/appreadings) interprets them. They lived in
// tenant/appreadings until 2026-11-16, which made cluster import the tenant.

type ActivityObservation struct {
	At      *time.Time
	Sources []string
	Err     error
}
type DataFreshnessDB struct {
	Name           string  `json:"name"`
	Environment    string  `json:"environment"`
	LastActivityTS *string `json:"last_activity_ts,omitempty"`
	// AgeDays is wall-clock age (now − last_activity). Informational only — do not drive Sync decisions.
	AgeDays *float64 `json:"age_days,omitempty"`
	// LagVsProdDays is max(0, prod_activity − target_activity) in days. Nil for bifrost_prod (reference).
	LagVsProdDays *float64 `json:"lag_vs_prod_days,omitempty"`
	// StaleDays mirrors LagVsProdDays for non-prod (backward-compatible Sync signal). Nil for prod reference.
	StaleDays   *float64 `json:"stale_days,omitempty"`
	Verdict     string   `json:"verdict"` // fresh | aging | stale | reference | unknown
	Detail      string   `json:"detail,omitempty"`
	Sources     []string `json:"sources,omitempty"`
	LastCloneAt *string  `json:"last_clone_at,omitempty"`
}

// ApplicationsSnapshot is what service readiness has gathered when it asks
// for the applications domain.
type ApplicationsSnapshot struct {
	Finalize                       func(string, string, []ServiceDependencyView, string) ServiceDomainView
	DeploymentReach                func(*appsv1.Deployment) (probe.Reachability, string)
	Deployments                    map[string]appsv1.Deployment
	IngressRoutes                  map[string]bool
	Environments                   []config.AppEnv
	Schedulable, IngressController ServiceDependencyView
}
