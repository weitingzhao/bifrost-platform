package planview

import (
	"fmt"
	"testing"
)

// A dense graph at the CUT's first-version limits. Related edges deliberately
// go both ways; they must not be confused with blocking cycles.
func largeDocument(t testing.TB) *Document {
	d := document(t)
	base := d.Items[0]
	d.Items = make([]Item, MaxNodes)
	for i := range d.Items {
		n := base
		n.Node = fmt.Sprintf("node-%d", i)
		n.Ordinal = i + 1
		order := i + 1
		n.Order = &order
		n.Dependencies = []Edge{}
		d.Items[i] = n
	}
	for i := range d.Items {
		for j := 1; j <= MaxEdges/MaxNodes; j++ {
			e := edge(d, i, (i+j)%len(d.Items))
			e.Relation = "related"
			d.Items[i].Dependencies = append(d.Items[i].Dependencies, e)
		}
	}
	return d
}

func TestGraphLimits(t *testing.T) {
	d := largeDocument(t)
	g, err := Resolve(d.Root, map[string]*Document{d.Root: d})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Mainline) != MaxNodes || len(g.Edges) != MaxEdges {
		t.Fatalf("graph truncated: %d nodes, %d edges", len(g.Mainline), len(g.Edges))
	}
	d.Items[0].Dependencies = append(d.Items[0].Dependencies, edge(d, 0, 6))
	if err := d.Validate(); err == nil {
		t.Fatal("accepted 2001 edges")
	}
	d = largeDocument(t)
	n := d.Items[0]
	n.Node = "extra"
	d.Items = append(d.Items, n)
	if err := d.Validate(); err == nil {
		t.Fatal("accepted 501 nodes")
	}
}

func TestImplementationGateDoesNotBlockDesign(t *testing.T) {
	d := document(t)
	g, err := Resolve(d.Root, map[string]*Document{d.Root: d})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range g.Edges {
		if e.From.Node == "4.4" && e.To.Node == "4.1" && e.Relation == "blocks" {
			found = true
			if e.RequiredFor != "implementation" {
				t.Fatalf("implementation gate expanded: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("lost the real implementation gate")
	}
}

func BenchmarkResolve500Nodes2000Edges(b *testing.B) {
	d := largeDocument(b)
	docs := map[string]*Document{d.Root: d}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Resolve(d.Root, docs); err != nil {
			b.Fatal(err)
		}
	}
}
