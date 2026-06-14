package topology

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

func TestBuild_ProdEdgeStatuses(t *testing.T) {
	topo := &config.TopologyFile{
		DeploymentPhase: "compose",
		Nodes: []config.TopologyNode{
			{ID: "mini-pc-a", Label: "a", Host: "192.168.10.70", Group: "linux", Grid: config.GridPos{1, 0}},
			{ID: "mini-pc-b", Label: "b", Host: "192.168.10.80", Group: "linux", Grid: config.GridPos{1, 2}},
		},
		Edges: []config.TopologyEdge{
			{
				ID: "pg", From: "mini-pc-a", To: "mini-pc-b", Label: "PG",
				Kind: "data", Environments: []string{"prod"}, MatrixTarget: "postgres",
			},
		},
	}
	env := config.Environment{
		ID: "prod", Label: "Prod", NginxBase: "http://192.168.10.70",
		Postgres: config.HostPort{Host: "192.168.10.80", Port: 5432},
		Redis:    config.HostPort{Host: "192.168.10.70", Port: 6379},
	}
	matrix := probe.MatrixResponse{
		Targets: []probe.Target{
			{ID: "postgres", Reachability: probe.ReachOK, Detail: "TCP open"},
		},
	}
	resp := Build(topo, env, matrix)
	if len(resp.Edges) != 1 {
		t.Fatalf("edges: %d", len(resp.Edges))
	}
	if resp.Edges[0].Status != probe.ReachOK {
		t.Fatalf("edge status: %s", resp.Edges[0].Status)
	}
	if len(resp.Nodes[0].MatrixServices) == 0 {
		t.Fatalf("expected matrix_services on mini-pc-a")
	}
}
