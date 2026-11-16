package cluster

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"sort"
)

func baselineNodeCoverage(nodes []NodeView, catalog []CapabilityCatalogEntry) []CapabilityCoverageView {
	nodeCapsByID := make(map[string][]NodeView)
	for _, n := range nodes {
		for _, c := range n.Capabilities {
			nodeCapsByID[c.ID] = append(nodeCapsByID[c.ID], n)
		}
	}

	out := make([]CapabilityCoverageView, 0)
	for _, entry := range catalog {
		if entry.Scope != "node" {
			continue
		}
		matched := nodeCapsByID[entry.ID]
		cv := CapabilityCoverageView{
			ID:          entry.ID,
			Label:       entry.Label,
			Category:    entry.Category,
			Scope:       entry.Scope,
			LabelHint:   entry.LabelHint,
			RequiredFor: entry.RequiredFor,
			NodeNames:   []string{},
		}
		for _, n := range matched {
			cv.NodesTotal++
			cv.NodeNames = append(cv.NodeNames, n.Name)
			if n.Reachability == probe.ReachOK && n.Status == "Ready" {
				cv.NodesReady++
			}
		}
		sort.Strings(cv.NodeNames)
		cv.Reachability, cv.GapReason = baselineCoverageReach(entry.ID, cv.NodesReady, cv.NodesTotal)
		out = append(out, cv)
	}
	return out
}

func baselineCoverageReach(id string, ready, total int) (probe.Reachability, string) {
	switch {
	case ready > 0:
		return probe.ReachOK, ""
	case total > 0:
		return probe.ReachDegraded, fmt.Sprintf("%s: %d node(s) labeled but none Ready", id, total)
	default:
		if id == "nfs-client" {
			return probe.ReachFail, "No node labeled storage.nfs/client=true — NFS PVCs will fail to mount"
		}
		return probe.ReachDegraded, "No nodes declare this capability yet"
	}
}
