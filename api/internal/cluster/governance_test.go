package cluster

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestBuildNodeCoverageNFSSClient(t *testing.T) {
	coverage := buildNodeCoverage([]NodeView{
		{
			Name: "n1", Status: "Ready", Reachability: probe.ReachOK,
			Capabilities: []NodeCapabilityView{{ID: "nfs-client", Label: "NFS client"}},
		},
		{Name: "n2", Status: "Ready", Reachability: probe.ReachOK},
	}, NodeCapabilityCatalog())

	var nfs *CapabilityCoverageView
	for i := range coverage {
		if coverage[i].ID == "nfs-client" {
			nfs = &coverage[i]
			break
		}
	}
	if nfs == nil {
		t.Fatal("nfs-client coverage missing")
	}
	if nfs.NodesReady != 1 || nfs.NodesTotal != 1 {
		t.Fatalf("nfs coverage: %+v", nfs)
	}
	if nfs.Reachability != probe.ReachOK {
		t.Fatalf("reach: %s", nfs.Reachability)
	}
}

func TestCoverageReachNoNFSNodes(t *testing.T) {
	reach, reason := coverageReach("nfs-client", 0, 0)
	if reach != probe.ReachFail {
		t.Fatalf("reach: %s", reach)
	}
	if reason == "" {
		t.Fatal("expected gap reason")
	}
}

func TestNodeCapabilityCatalogIncludesClusterScope(t *testing.T) {
	catalog := NodeCapabilityCatalog()
	var nodeCount, clusterCount int
	for _, e := range catalog {
		switch e.Scope {
		case "node":
			nodeCount++
		case "cluster":
			clusterCount++
		}
	}
	if nodeCount < 5 || clusterCount < 4 {
		t.Fatalf("catalog counts node=%d cluster=%d", nodeCount, clusterCount)
	}
}

func TestGovernanceMissingKubeconfig(t *testing.T) {
	t.Setenv("PLATFORM_KUBECONFIG", t.TempDir()+"/missing.yaml")
	svc := NewService(nil)
	resp := svc.Governance(t.Context())
	if len(resp.ClusterCaps) == 0 {
		t.Fatal("expected cluster caps even on failure")
	}
	if resp.ClusterCaps[0].Reachability != probe.ReachFail {
		t.Fatalf("expected fail on cluster caps: %+v", resp.ClusterCaps[0])
	}
}
