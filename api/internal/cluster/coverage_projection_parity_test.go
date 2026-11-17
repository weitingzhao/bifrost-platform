//bifrost:owner foundation

package cluster

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"reflect"
	"testing"
)

func TestNodeCoverageMatchesOriginal(t *testing.T) {
	catalogs := [][]CapabilityCatalogEntry{nil, {}, {{ID: "nfs-client", Scope: "node"}}, {{ID: "other", Scope: "node"}}, {{ID: "other", Scope: "cluster"}, {ID: "nfs-client", Scope: "node", Label: "NFS", Category: "data", LabelHint: "hint", RequiredFor: "volumes"}, {ID: "other", Scope: "node"}, {ID: "nfs-client", Scope: "node"}}}
	for _, catalog := range catalogs {
		for _, state := range []string{"Ready", "NotReady", "", "ready"} {
			for _, reach := range []probe.Reachability{probe.ReachOK, probe.ReachFail, probe.ReachDegraded, probe.ReachUnknown, ""} {
				for mode := 0; mode < 4; mode++ {
					var nodes []NodeView
					if mode > 0 {
						nodes = append(nodes, NodeView{Name: "z-node", Status: state, Reachability: reach, Capabilities: []NodeCapabilityView{{ID: "nfs-client"}, {ID: "other"}, {ID: "nfs-client"}}})
					}
					if mode > 1 {
						nodes = append(nodes, NodeView{Name: "a-node", Status: "Ready", Reachability: probe.ReachOK, Capabilities: []NodeCapabilityView{{ID: "nfs-client"}}})
					}
					if mode > 2 {
						nodes = append(nodes, NodeView{Name: "a-node", Status: state, Reachability: reach})
					}
					want, got := baselineNodeCoverage(nodes, catalog), buildNodeCoverage(nodes, catalog)
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("state=%s reach=%s mode=%d: %#v != %#v", state, reach, mode, want, got)
					}
				}
			}
		}
	}
}
