package planview

import (
	"fmt"
	"slices"
	"sort"
)

// NodeRef keeps the root explicit; logical node labels are not database IDs.
type NodeRef struct {
	Root string `json:"root"`
	Node string `json:"node"`
}

type Dependency struct {
	From        NodeRef  `json:"from"`
	To          NodeRef  `json:"to"`
	Milestone   string   `json:"milestone"`
	RequiredFor string   `json:"required_for"`
	Relation    string   `json:"relation"`
	Reason      string   `json:"reason"`
	Sources     []string `json:"sources"`
}

type Graph struct {
	Mainline []NodeRef    `json:"mainline"`
	Edges    []Dependency `json:"edges"`
}

// Resolve checks the complete set of referenced documents before returning
// a root's display graph. The caller must supply documents from one locked
// database snapshot; this pure function does not establish database identity
// or adoption. In particular, mainline order, anchors and parenthood create
// no blocking edges, and a parent's implementation gate is not inherited by
// a design or an early slice.
func Resolve(root string, documents map[string]*Document) (Graph, error) {
	out := Graph{Mainline: []NodeRef{}, Edges: []Dependency{}}
	start := documents[root]
	if start == nil {
		return out, fmt.Errorf("missing root %s", root)
	}
	items := map[NodeRef]Item{}
	for key, doc := range documents {
		if doc == nil || key != doc.Root {
			return out, fmt.Errorf("mismatched document root %s", key)
		}
		if doc.Tenant != start.Tenant {
			return out, fmt.Errorf("cross-tenant root %s", key)
		}
		if err := doc.Validate(); err != nil {
			return out, fmt.Errorf("%s: %w", key, err)
		}
		for _, n := range doc.Items {
			items[NodeRef{key, n.Node}] = n
		}
	}
	graph := map[NodeRef][]NodeRef{}
	for ref, n := range items {
		graph[ref] = nil
		for _, e := range n.Dependencies {
			to := NodeRef{e.Root, e.Node}
			target, exists := items[to]
			if !exists || !slices.Contains(target.Milestones, e.Milestone) {
				return out, fmt.Errorf("dangling dependency %s/%s/%s", e.Root, e.Node, e.Milestone)
			}
			if ref == to {
				return out, fmt.Errorf("self dependency: %v", ref)
			}
			if e.Relation == "blocks" {
				graph[ref] = append(graph[ref], to)
			}
		}
	}
	states := map[NodeRef]int{}
	var visit func(NodeRef) error
	visit = func(ref NodeRef) error {
		if states[ref] == 1 {
			return fmt.Errorf("cross-root blocking cycle at %s/%s", ref.Root, ref.Node)
		}
		if states[ref] == 2 {
			return nil
		}
		states[ref] = 1
		for _, to := range graph[ref] {
			if err := visit(to); err != nil {
				return err
			}
		}
		states[ref] = 2
		return nil
	}
	for ref := range graph {
		if err := visit(ref); err != nil {
			return out, err
		}
	}
	for _, n := range start.Items {
		if n.Lane == "mainline" {
			out.Mainline = append(out.Mainline, NodeRef{root, n.Node})
		}
		for _, e := range n.Dependencies {
			out.Edges = append(out.Edges, Dependency{NodeRef{root, n.Node}, NodeRef{e.Root, e.Node}, e.Milestone, e.RequiredFor, e.Relation, e.Reason, slices.Clone(e.Sources)})
		}
	}
	sort.Slice(out.Mainline, func(i, j int) bool { return *items[out.Mainline[i]].Order < *items[out.Mainline[j]].Order })
	return out, nil
}
