package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/tenant/appreadings"
)

// The package's tests read the applications and the data as the server
// wires them: with the tenant's readings bound.
func init() { bindTenantReadings() }

func bindTenantReadings() {
	BindAppReadings(AppReadings{ApplicationsDomain: appreadings.EvalApplicationsDomain, FreshnessDatabases: appreadings.FreshnessDatabases})
}

// Origin: 2026-11-16, cluster stopped importing tenant/appreadings (the
// foundation wall) and reads the applications through AppReadings. Unbound,
// it must not report the applications ready (the first version of this
// answered an unknown dependency, which the domain status counts as
// satisfied: this test caught it).
// The other tests of this package are the positive control: bound, the
// domain is evaluated.
func TestApplicationsDomainIsUnknownWithoutAReader(t *testing.T) {
	BindAppReadings(AppReadings{})
	t.Cleanup(bindTenantReadings)
	d := evalApplicationsDomain(applicationsReadySnapshot())
	if d.Reachability != probe.ReachFail || d.Status != "unavailable" || len(d.Dependencies) != 1 || d.Dependencies[0].ID != "application-readings" {
		t.Fatalf("unbound: %+v", d)
	}
	bindTenantReadings()
	if d = evalApplicationsDomain(applicationsReadySnapshot()); d.Reachability != probe.ReachOK {
		t.Fatalf("bound: %+v", d)
	}
}
