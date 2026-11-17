package cluster

import (
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// AppReadings is how the tenant's applications and data are read from what
// this package gathers. The tenant domain owns the reading (which
// deployments make an application ready, when a database is stale); this
// package declares the shape and the server binds tenant/appreadings. Until
// 2026-11-16 this package imported the tenant's package and called it.
type AppReadings struct {
	// ApplicationsDomain is the applications domain of service readiness.
	ApplicationsDomain func(resourceview.ApplicationsSnapshot) ServiceDomainView
	// FreshnessDatabases fills each database's lag, age and verdict from the
	// observations of last activity.
	FreshnessDatabases func(databases []DataFreshnessDB, observed []resourceview.ActivityObservation, referenceDB string, lastCloneAt *string, now time.Time) []DataFreshnessDB
}

// appReadings is bound once, at start-up, before anything is served. It is
// the package's and not a Service's because the readiness evaluation is a
// function of a snapshot, as its tests use it.
var appReadings AppReadings

// BindAppReadings sets the reader. Without one the applications domain is
// unavailable and the databases carry no verdict.
func BindAppReadings(r AppReadings) { appReadings = r }
