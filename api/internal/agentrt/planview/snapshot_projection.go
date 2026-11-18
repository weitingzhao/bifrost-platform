package planview

// ProjectSnapshotNodes applies an already accepted view to its display nodes.
// Adoption validation and verification remain with the caller.
func ProjectSnapshotNodes(nodes []map[string]any, document *Document, warningText string, bindings map[string]string) (Graph, []Item, []string) {
	graph := Graph{Mainline: []NodeRef{}, Edges: []Dependency{}}
	items := []Item{}
	warnings := []string{}
	if warning := warningText; warning != "" {
		warnings = append(warnings, warning)
	} else if doc := document; doc != nil {
		graph = adoptedGraph(doc)
		items = doc.Items
	} else {
		warnings = append(warnings, "Legacy evidence: plan has no adopted view; nodes are Unclassified")
	}
	byID := map[string]map[string]any{}
	for _, n := range nodes {
		n["lane_kind"] = "Unclassified"
		byID[n["plan_node_id"].(string)] = n
	}
	for _, item := range items {
		if n := byID[bindings[item.Node]]; n != nil {
			n["lane_kind"] = item.Lane
			n["view"] = item
		}
	}
	return graph, items, warnings
}
