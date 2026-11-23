package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The package's tests read the applications and the compute domains as the
// server wires them: with the tenant's and compute's readings bound. Since
// 2026-11-19 (cluster is foundation's alone) the two flow fixtures
// readings_flow_tenant_test.go and readings_flow_compute_test.go bind them,
// from package cluster_test, before any test runs.

// Origin: 2026-11-16, the GPU and warehouse domains are read through
// ComputeReadings (the functions moved to elasticreadings). Unbound, they
// must not read as ready or as standby. The other tests of this package are
// the positive control: bound, the domains are evaluated as before.
func TestElasticDomainsAreUnavailableWithoutAReader(t *testing.T) {
	bound := computeReadings
	if bound.GPUDomain == nil {
		t.Fatal("the compute flow fixture bound no readings (readings_flow_compute_test.go)")
	}
	BindComputeReadings(ComputeReadings{})
	t.Cleanup(func() { BindComputeReadings(bound) })
	for _, d := range []ServiceDomainView{evalGPUDomain(readinessSnapshot{}), evalWorkersDomain(readinessSnapshot{})} {
		if d.Reachability != probe.ReachFail || d.Status != "unavailable" {
			t.Fatalf("unbound: %+v", d)
		}
	}
}

// Origin: 2026-11-16, cluster stopped importing tenant/appreadings (the
// foundation wall) and reads the applications through AppReadings. Unbound,
// it must not report the applications ready (the first version of this
// answered an unknown dependency, which the domain status counts as
// satisfied: this test caught it).
// The other tests of this package are the positive control: bound, the
// domain is evaluated.
func TestApplicationsDomainIsUnknownWithoutAReader(t *testing.T) {
	bound := appReadings
	if bound.ApplicationsDomain == nil {
		t.Fatal("the tenant flow fixture bound no readings (readings_flow_tenant_test.go)")
	}
	BindAppReadings(AppReadings{})
	t.Cleanup(func() { BindAppReadings(bound) })
	d := evalApplicationsDomain(applicationsReadySnapshot())
	if d.Reachability != probe.ReachFail || d.Status != "unavailable" || len(d.Dependencies) != 1 || d.Dependencies[0].ID != "application-readings" {
		t.Fatalf("unbound: %+v", d)
	}
	BindAppReadings(bound)
	if d = evalApplicationsDomain(applicationsReadySnapshot()); d.Reachability != probe.ReachOK {
		t.Fatalf("bound: %+v", d)
	}
}
