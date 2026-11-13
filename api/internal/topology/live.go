package topology

import (
	"net"
	"sort"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

// Live is what the cluster and Prometheus say about the machines right now.
// topology.yaml keeps the drawing (ids, labels, grid, edges); which machines
// exist and which of them are cluster nodes is read, not typed. The file had
// said ubt-k3s-06 was in the cluster for weeks after it left, and never knew
// ubt-k3s-07 or the NAS.
type Live struct {
	// ClusterKnown is false when the node list could not be read; the file's
	// in_k3s_cluster is then left as written rather than turned to false.
	ClusterKnown bool
	// ClusterNodes maps a node's internal IP to its name.
	ClusterNodes map[string]string
	// Scraped maps the IP of every host Prometheus scrapes with a node
	// exporter to its host name. Empty when Prometheus could not be asked.
	Scraped map[string]string
}

// applyLive corrects in_k3s_cluster from the node list and appends the
// machines the file does not draw: cluster nodes first, then hosts that are
// only scraped. Appended nodes sit on a new row under the drawn ones.
func applyLive(nodes []NodeView, live Live) []NodeView {
	drawn := map[string]bool{}
	maxRow := 0
	for i := range nodes {
		n := &nodes[i]
		if n.Grid.Row > maxRow {
			maxRow = n.Grid.Row
		}
		if net.ParseIP(n.Host) == nil {
			continue
		}
		drawn[n.Host] = true
		if live.ClusterKnown {
			_, in := live.ClusterNodes[n.Host]
			n.InK3sCluster = in
		}
	}

	col := 0
	add := func(ip, name string, inCluster bool, detail string) {
		nodes = append(nodes, NodeView{
			ID: name, Label: name, Host: ip, Group: "linux",
			ComposeRoles: []string{}, K3sRoles: []string{},
			InK3sCluster: inCluster, Discovered: true,
			Grid:   config.GridPos{Row: maxRow + 1, Col: col},
			Status: probe.ReachUnknown, Detail: detail,
			MatrixServices: []ServiceView{},
		})
		drawn[ip] = true
		col++
	}
	if live.ClusterKnown {
		for _, ip := range sortedKeys(live.ClusterNodes) {
			if !drawn[ip] {
				add(ip, live.ClusterNodes[ip], true, "cluster node, read from the cluster; not drawn in topology.yaml")
			}
		}
	}
	for _, ip := range sortedKeys(live.Scraped) {
		if !drawn[ip] {
			add(ip, live.Scraped[ip], false, "host scraped by Prometheus; not drawn in topology.yaml")
		}
	}
	return nodes
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
