package cluster

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// AppReadings is how the tenant's applications are read from what this
// package gathers. The tenant domain owns the reading (which deployments
// make an application ready); this package declares the shape and the server
// binds tenant/appreadings. Until 2026-11-16 this package imported the
// tenant's package and called it. The reading of the databases' freshness
// left with the data freshness for package dataclone on 2026-11-17, which
// calls tenant/appreadings itself.
type AppReadings struct {
	// ApplicationsDomain is the applications domain of service readiness.
	ApplicationsDomain func(resourceview.ApplicationsSnapshot) ServiceDomainView
}

// appReadings is bound once, at start-up, before anything is served. It is
// the package's and not a Service's because the readiness evaluation is a
// function of a snapshot, as its tests use it.
var appReadings AppReadings

// BindAppReadings sets the reader. Without one the applications domain is
// unavailable.
func BindAppReadings(r AppReadings) { appReadings = r }

// ComputeReadings is how the compute domains of service readiness (GPU, data
// warehouse, general workers) are read from what this package gathers: when a pool
// that is powered off is standby and when it is missing compute is the
// compute domain's reading. The server binds elasticreadings. Until
// 2026-11-16 those functions were this package's.
type ComputeReadings struct {
	GPUDomain       func(resourceview.ElasticSnapshot) ServiceDomainView
	WarehouseDomain func(resourceview.ElasticSnapshot) ServiceDomainView
	WorkersDomain   func(resourceview.ElasticSnapshot) ServiceDomainView
	// NodeMode says whether an on-demand node is active, standby or
	// degraded, with why (resourceview.ElasticNode* modes).
	NodeMode func(resourceview.ElasticNodeObservation) (mode, detail string)
}

// computeReadings is bound once, at start-up, as appReadings is.
var computeReadings ComputeReadings

// BindComputeReadings sets the reader. Without one the three domains are
// unavailable.
func BindComputeReadings(r ComputeReadings) { computeReadings = r }
