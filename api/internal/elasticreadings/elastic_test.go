package elasticreadings_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/elasticreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"github.com/weitingzhao/bifrost-platform/api/internal/resourceview"
)

// snapshot is what cluster hands over, with one GPU node in the given mode
// and the ollama deployment wanting the given replicas. The four dependency
// readings answer as cluster's would for a pool and a workload that are off.
func snapshot(mode, status string, replicas int32) resourceview.ElasticSnapshot {
	off := func(id, label string) resourceview.ServiceDependencyView {
		return resourceview.ServiceDependencyView{ID: id, Label: label, Reachability: probe.ReachDegraded, Detail: "scaled to zero"}
	}
	return resourceview.ElasticSnapshot{
		Nodes: []resourceview.ElasticNode{{Name: "gpu-server", Status: status, ElasticMode: mode,
			Reachability: probe.ReachOK, CapabilityIDs: []string{"gpu-pool", "warehouse"}}},
		NodeCoverage:    map[string]resourceview.ElasticCoverage{"warehouse": {NodesReady: 0, NodesTotal: 1, NodeNames: []string{"gpu-server"}}},
		DesiredReplicas: func(string, string) int32 { return replicas },
		PoolDep:         func(id, label string) resourceview.ServiceDependencyView { return off("pool-"+id, label) },
		NodeCovDep:      off,
		DeploymentDep:   func(ns, name, label string) resourceview.ServiceDependencyView { return off("workload-"+name, label) },
		Finalize: func(id, label string, deps []resourceview.ServiceDependencyView, purpose string) resourceview.ServiceDomainView {
			return resourceview.ServiceDomainView{ID: id, Label: label, Status: "partial", Reachability: probe.ReachDegraded, Dependencies: deps}
		},
	}
}

// Origin: 2026-11-16, the elastic readings left cluster for this package.
// This is their contract from outside, the rule the functions exist for: a
// node that is powered off is standby and ok while nothing needs it, and
// missing compute once something does. cluster's own tests still run the
// same functions through the binding, with real snapshots.
func TestStandbyIsOkUntilSomethingNeedsTheCompute(t *testing.T) {
	for _, domain := range []struct {
		name string
		read func(resourceview.ElasticSnapshot) resourceview.ServiceDomainView
	}{{"gpu", elasticreadings.GPUDomain}, {"warehouse", elasticreadings.WarehouseDomain}} {
		idle := domain.read(snapshot("standby", "NotReady", 0))
		if idle.ID != domain.name || idle.Status != "standby" || idle.Reachability != probe.ReachOK {
			t.Errorf("%s, powered off and not needed: %+v", domain.name, idle)
		}
		needed := domain.read(snapshot("standby", "NotReady", 1))
		if needed.Status != "unavailable" || needed.Reachability != probe.ReachFail {
			t.Errorf("%s, powered off and needed: %+v", domain.name, needed)
		}
		// The positive control: a node that is up is neither, and the
		// domain is what its dependencies made it.
		up := domain.read(snapshot("active", "Ready", 1))
		if up.Status != "partial" || up.Reachability != probe.ReachDegraded {
			t.Errorf("%s, node up: %+v", domain.name, up)
		}
	}
}
