// Package resourceview contains pure resource observation projections.
package resourceview

import (
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
	"sort"
)

type CoverageNode struct {
	Name, Status string
	Reachability probe.Reachability
	Capabilities []CoverageCapability
}
type CoverageCapability struct{ ID string }
type CoverageCatalogEntry struct{ ID, Label, Category, Scope, LabelHint, RequiredFor string }
type Coverage struct {
	ID           string             `json:"id"`
	Label        string             `json:"label"`
	Category     string             `json:"category"`
	Scope        string             `json:"scope"`
	LabelHint    string             `json:"label_hint,omitempty"`
	RequiredFor  string             `json:"required_for,omitempty"`
	NodesReady   int                `json:"nodes_ready"`
	NodesTotal   int                `json:"nodes_total"`
	NodeNames    []string           `json:"node_names"`
	Reachability probe.Reachability `json:"reachability"`
	GapReason    string             `json:"gap_reason,omitempty"`
}

func NodeCoverage(nodes []CoverageNode, catalog []CoverageCatalogEntry) []Coverage {
	nodeCapsByID := make(map[string][]CoverageNode)
	for _, n := range nodes {
		for _, c := range n.Capabilities {
			nodeCapsByID[c.ID] = append(nodeCapsByID[c.ID], n)
		}
	}

	out := make([]Coverage, 0)
	for _, entry := range catalog {
		if entry.Scope != "node" {
			continue
		}
		matched := nodeCapsByID[entry.ID]
		cv := Coverage{
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
		cv.Reachability, cv.GapReason = CoverageReach(entry.ID, cv.NodesReady, cv.NodesTotal)
		out = append(out, cv)
	}
	return out
}

func CoverageReach(id string, ready, total int) (probe.Reachability, string) {
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
