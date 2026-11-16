package topologyview

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"testing"
)

func TestProjectionUsesOnlySuppliedDisplayAdapter(t *testing.T) {
	topo := &config.TopologyFile{Nodes: []config.TopologyNode{{ID: "custom", Host: "example.invalid"}}, Edges: []config.TopologyEdge{{ID: "edge", Kind: "custom"}}}
	env := config.Environment{ID: "fixture"}
	matrix := probe.MatrixResponse{Targets: []probe.Target{{ID: "custom-target", Reachability: probe.ReachFail, Detail: "unreachable"}}}
	bare := Build(topo, env, matrix, Live{}, Adapter{})
	if len(bare.Nodes[0].MatrixServices) != 0 || bare.Edges[0].Detail != "static link" {
		t.Fatalf("unexpected implicit mapping: %+v", bare)
	}
	injected := Build(topo, env, matrix, Live{}, Adapter{
		TargetNodes: func(id, environment string) []string {
			if id != "custom-target" || environment != "fixture" {
				t.Fatal("wrong adapter input")
			}
			return []string{"custom"}
		},
		EdgeDetail: func(kind string) (string, bool) { return "adapter-only detail", kind == "custom" },
	})
	if injected.Nodes[0].Status != probe.ReachFail || len(injected.Nodes[0].MatrixServices) != 1 || injected.Edges[0].Detail != "adapter-only detail" {
		t.Fatalf("adapter not projected: %+v", injected)
	}
	if injected.Edges[0].Status != probe.ReachUnknown {
		t.Fatal("static adapter must not manufacture successful observation")
	}
}
