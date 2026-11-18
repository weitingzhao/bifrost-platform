package planview

import "sort"

// Adoption validates the complete graph with planview.Resolve. Reading keeps
// this root's validated items and edges even if a dependency's document cannot
// be read now; its milestone evaluates Unknown through the same facts snapshot.
func adoptedGraph(d *Document) Graph {
	g := Graph{Mainline: []NodeRef{}, Edges: []Dependency{}}
	order := map[string]int{}
	for _, n := range d.Items {
		from := NodeRef{Root: d.Root, Node: n.Node}
		if n.Lane == "mainline" {
			g.Mainline = append(g.Mainline, from)
			order[n.Node] = *n.Order
		}
		for _, e := range n.Dependencies {
			g.Edges = append(g.Edges, Dependency{From: from, To: NodeRef{Root: e.Root, Node: e.Node}, Milestone: e.Milestone, RequiredFor: e.RequiredFor, Relation: e.Relation, Reason: e.Reason, Sources: e.Sources})
		}
	}
	sort.Slice(g.Mainline, func(i, j int) bool { return order[g.Mainline[i].Node] < order[g.Mainline[j].Node] })
	return g
}

func ItemIndex(d *Document, node string) int {
	for i, n := range d.Items {
		if n.Node == node {
			return i
		}
	}
	return 0
}
