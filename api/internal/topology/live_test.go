package topology

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// The file as it stood on 2026-11-13: ubt-k3s-06 written as a cluster node
// (it left on 2026-10-17), no ubt-k3s-07, no NAS, Win11 with no address.
func staleFile() *config.TopologyFile {
	return &config.TopologyFile{
		DeploymentPhase: "k3s_partial",
		Nodes: []config.TopologyNode{
			{ID: "win11-host", Label: "Win11 Host", Group: "external", Grid: config.GridPos{Row: 0, Col: 1}},
			{ID: "mini-pc-a", Label: "mini-pc-a", Host: "192.168.10.70", Group: "linux", InK3sCluster: true, Grid: config.GridPos{Row: 1, Col: 0}},
			{ID: "ubt-k3s-06", Label: "ubt-k3s-06", Host: "192.168.10.79", Group: "linux", InK3sCluster: true, Grid: config.GridPos{Row: 1, Col: 2}},
			{ID: "mac-mini-1", Label: "Mac mini", Host: "192.168.10.50", Group: "mac", Grid: config.GridPos{Row: 2, Col: 0}},
		},
	}
}

func byID(resp Response) map[string]NodeView {
	out := map[string]NodeView{}
	for _, n := range resp.Nodes {
		out[n.ID] = n
	}
	return out
}

func TestLiveCorrectsTheFileAndAddsWhatItDoesNotDraw(t *testing.T) {
	live := Live{
		ClusterKnown: true,
		ClusterNodes: map[string]string{"192.168.10.70": "ubt-k3s-02", "192.168.10.80": "ubt-k3s-07"},
		Scraped: map[string]string{
			"192.168.10.70": "ubt-k3s-02", "192.168.10.80": "ubt-k3s-07",
			"192.168.10.79": "ubt-k3s-06", "192.168.10.50": "ops-mac-agent-02", "192.168.10.20": "UGREEN-NAS",
		},
	}
	resp := Build(staleFile(), config.Environment{ID: "prod"}, probe.MatrixResponse{}, live)
	nodes := byID(resp)

	if nodes["ubt-k3s-06"].InK3sCluster {
		t.Error("ubt-k3s-06 is not in the node list but is still shown as a cluster node")
	}
	if !nodes["mini-pc-a"].InK3sCluster || nodes["mini-pc-a"].Discovered {
		t.Errorf("mini-pc-a is a drawn cluster node: %+v", nodes["mini-pc-a"])
	}
	seven, ok := nodes["ubt-k3s-07"]
	if !ok || !seven.InK3sCluster || !seven.Discovered || seven.Host != "192.168.10.80" || seven.Grid.Row != 3 {
		t.Errorf("ubt-k3s-07 should be added as a discovered cluster node on a new row: %+v (present %v)", seven, ok)
	}
	nas, ok := nodes["UGREEN-NAS"]
	if !ok || nas.InK3sCluster || !nas.Discovered {
		t.Errorf("the NAS should be added as a discovered host outside the cluster: %+v (present %v)", nas, ok)
	}
	if len(resp.Nodes) != 6 {
		t.Errorf("got %d nodes, want the 4 drawn plus ubt-k3s-07 and the NAS", len(resp.Nodes))
	}
	if nodes["win11-host"].InK3sCluster || nodes["mac-mini-1"].InK3sCluster {
		t.Error("a machine outside the cluster became a cluster node")
	}
	if seven.Grid == nas.Grid {
		t.Error("two discovered machines share one grid cell")
	}
}

func TestWithoutTheNodeListTheFileIsLeftAsWritten(t *testing.T) {
	// A local run with no cluster, or an API server that did not answer:
	// "not read" must not be shown as "not a cluster node".
	resp := Build(staleFile(), config.Environment{ID: "prod"}, probe.MatrixResponse{}, Live{})
	nodes := byID(resp)
	if !nodes["mini-pc-a"].InK3sCluster || !nodes["ubt-k3s-06"].InK3sCluster || len(resp.Nodes) != 4 {
		t.Errorf("with nothing read the answer should be the file: %+v", resp.Nodes)
	}
}
