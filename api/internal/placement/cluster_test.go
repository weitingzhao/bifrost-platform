package placement

import (
	"reflect"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Origin: 2026-11-16, cluster stopped importing this package (the foundation
// wall) and reads the pools through cluster.PoolEvaluator. This holds that
// the adapter answers what cluster.Service.Placement answered before: the
// same document as Evaluate, a node list that was not read ok carried into
// it, and each pool's count and status for service readiness.
func TestClusterPoolsIsEvaluateInTheClustersShape(t *testing.T) {
	nodes := []cluster.PoolNode{
		{Name: "a", Architecture: "amd64", Reachability: probe.ReachOK},
		{Name: "b", Architecture: "arm64", Reachability: probe.ReachFail},
	}
	in := []NodeInput{
		{Name: "a", Architecture: "amd64", Reachability: probe.ReachOK},
		{Name: "b", Architecture: "arm64", Reachability: probe.ReachFail},
	}
	want := Evaluate("c1", in)
	got := ClusterPools("c1", nodes, probe.ReachOK, "")
	body, ok := got.Body.(Response)
	if !ok {
		t.Fatalf("the document is %T, want Response", got.Body)
	}
	body.GeneratedAt = want.GeneratedAt
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("document = %+v, want %+v", body, want)
	}
	if len(got.Pools) != len(want.Pools) || len(got.Pools) == 0 {
		t.Fatalf("%d readings for %d pools", len(got.Pools), len(want.Pools))
	}
	for i, p := range want.Pools {
		r := got.Pools[i]
		if r.ID != p.ID || r.Status != string(p.Status) || r.NodesReady != p.NodesReady || r.NodesTotal != p.NodesTotal {
			t.Errorf("pool %s: reading %+v", p.ID, r)
		}
	}
	// The statuses readiness tells apart are this package's words.
	if cluster.PoolPlanned != string(PoolStatusPlanned) || cluster.PoolDegraded != string(PoolStatusDegraded) {
		t.Fatal("cluster's pool statuses are not placement's")
	}
	down := ClusterPools("c1", nodes, probe.ReachFail, "api unreachable").Body.(Response)
	if down.Reachability != probe.ReachFail || down.Detail != "api unreachable" {
		t.Fatalf("a failed node read is not carried: %+v", down)
	}
}
