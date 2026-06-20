package placement

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestAmd64CIReady(t *testing.T) {
	if Amd64CIReady([]NodeInput{{Architecture: "arm64", Reachability: probe.ReachOK}}) {
		t.Fatal("arm64 only should not be CI ready")
	}
	if !Amd64CIReady([]NodeInput{{Architecture: "amd64", Reachability: probe.ReachOK}}) {
		t.Fatal("amd64 ready should be CI ready")
	}
}

func TestEvaluateCriticalViolationWithoutAmd64(t *testing.T) {
	resp := Evaluate("test", []NodeInput{
		{Name: "arm-1", Architecture: "arm64", Reachability: probe.ReachOK},
	})
	if resp.Reachability != probe.ReachFail {
		t.Fatalf("reach: %s", resp.Reachability)
	}
	found := false
	for _, v := range resp.Violations {
		if v.Code == "amd64_ci_unavailable" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected amd64_ci_unavailable violation")
	}
}

func TestEvaluateNFSPool(t *testing.T) {
	resp := Evaluate("test", []NodeInput{
		{Name: "n1", Architecture: "amd64", CapabilityIDs: []string{"nfs-client"}, Reachability: probe.ReachOK},
		{Name: "n2", Architecture: "amd64", Reachability: probe.ReachOK},
	})
	pool := findPool(resp.Pools, "nfs_client")
	if pool == nil || pool.NodesReady != 1 {
		t.Fatalf("nfs_client pool: %+v", pool)
	}
}

func findPool(pools []PoolView, id string) *PoolView {
	for i := range pools {
		if pools[i].ID == id {
			return &pools[i]
		}
	}
	return nil
}

func TestCIPreflightReason(t *testing.T) {
	if r := CIPreflightReason([]NodeInput{{Architecture: "amd64", Reachability: probe.ReachOK}}); r != "" {
		t.Fatalf("unexpected reason: %s", r)
	}
	if r := CIPreflightReason(nil); r == "" {
		t.Fatal("expected reason for empty nodes")
	}
}
