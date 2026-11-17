package placement

import (
	"strings"
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

// 2026-11-17 audit row #12 (LANE-BIZ-ALIGN): the gpu pool was written as "planned" on
// gpu-server whatever the nodes said; its status now comes from nodes labelled workload=gpu.
func TestGPUPoolStatusFromNodeLabels(t *testing.T) {
	none := findPool(Evaluate("t", []NodeInput{{Name: "a", Architecture: "amd64", Reachability: probe.ReachOK}}).Pools, "gpu")
	if none == nil || none.Status != PoolStatusDegraded || none.NodesTotal != 0 {
		t.Fatalf("gpu pool without labelled nodes: %+v", none)
	}
	live := findPool(Evaluate("t", []NodeInput{{Name: "g", Architecture: "amd64", WorkloadLabel: "gpu", Reachability: probe.ReachOK}}).Pools, "gpu")
	if live == nil || live.Status != PoolStatusLive || live.NodesReady != 1 {
		t.Fatalf("gpu pool with a Ready labelled node: %+v", live)
	}
	off := Evaluate("t", []NodeInput{{Name: "g", Architecture: "amd64", WorkloadLabel: "gpu", Reachability: probe.ReachFail}})
	if p := findPool(off.Pools, "gpu"); p == nil || p.Status != PoolStatusDegraded || p.NodesTotal != 1 {
		t.Fatalf("gpu pool with a NotReady labelled node: %+v", p)
	}
	found := false
	for _, v := range off.Violations {
		if v.Code == "gpu_pool_not_ready" && strings.Contains(v.Message, "g") {
			found = true
		}
	}
	if !found {
		t.Fatalf("violations: %+v", off.Violations)
	}
}
