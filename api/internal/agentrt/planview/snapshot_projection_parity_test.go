package planview

import (
	"reflect"
	"testing"
)

// Origin: moved from store/snapshot_projection_parity_test.go (TestSnapshotDisplayFrozenParity, the plan view half), 2026-11-18 (TD-582).
func TestSnapshotDisplayFrozenParity(t *testing.T) {
	t.Parallel()
	order := 2
	doc := &Document{Root: "R", Items: []Item{{Node: "one", Lane: "mainline", Order: &order}, {Node: "two", Lane: "support"}, {Node: "absent", Lane: "mainline", Order: &order}}}
	for _, document := range []*Document{nil, doc} {
		for _, warning := range []string{"", "unavailable"} {
			nodes := func() []map[string]any {
				return []map[string]any{{"plan_node_id": "n1"}, {"plan_node_id": "n2"}, {"plan_node_id": "unbound"}}
			}
			old, new := nodes(), nodes()
			bindings := map[string]string{"one": "n1", "two": "n2"}
			graph := Graph{Mainline: []NodeRef{}, Edges: []Dependency{}}
			items := []Item{}
			warnings := []string{}
			if warning != "" {
				warnings = append(warnings, warning)
			} else if document != nil {
				graph = adoptedGraph(document)
				items = document.Items
			} else {
				warnings = append(warnings, "Legacy evidence: plan has no adopted view; nodes are Unclassified")
			}
			byID := map[string]map[string]any{}
			for _, n := range old {
				n["lane_kind"] = "Unclassified"
				byID[n["plan_node_id"].(string)] = n
			}
			for _, item := range items {
				if n := byID[bindings[item.Node]]; n != nil {
					n["lane_kind"] = item.Lane
					n["view"] = item
				}
			}
			gotGraph, gotItems, gotWarnings := ProjectSnapshotNodes(new, document, warning, bindings)
			if !reflect.DeepEqual(old, new) || !reflect.DeepEqual(graph, gotGraph) || !reflect.DeepEqual(items, gotItems) || !reflect.DeepEqual(warnings, gotWarnings) {
				t.Fatal("snapshot projection differs")
			}
		}
	}
}
