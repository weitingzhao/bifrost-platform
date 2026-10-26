package planview

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fixture(t testing.TB) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/r4-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func document(t testing.TB) *Document {
	t.Helper()
	d, err := Parse(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPV0Artifact(t *testing.T) {
	d := document(t)
	if len(d.Items) != 12 || d.Root != "R4" || d.Version != 1 {
		t.Fatalf("unexpected document: %+v", d)
	}
	g, err := Resolve("R4", map[string]*Document{"R4": d})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, n := range g.Mainline {
		got = append(got, n.Node)
	}
	if strings.Join(got, ",") != "4.1,4.2,4.4,4.10" {
		t.Fatalf("mainline: %v", got)
	}
	for _, edge := range g.Edges {
		if edge.From.Node == "4.5.pv" && edge.Relation == "blocks" {
			t.Fatalf("early slice incorrectly blocked: %+v", edge)
		}
		if edge.From.Node == "4.4" && edge.To.Node == "4.2" && edge.Relation == "blocks" {
			t.Fatalf("narrative became blocking: %+v", edge)
		}
	}
}

func TestClosedShape(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"unknown root field":     func(m map[string]any) { m["execution"] = "done" },
		"missing version":        func(m map[string]any) { delete(m, "v") },
		"null version":           func(m map[string]any) { m["v"] = nil },
		"null items":             func(m map[string]any) { m["items"] = nil },
		"missing nullable field": func(m map[string]any) { delete(m["items"].([]any)[0].(map[string]any), "anchor_node_id") },
		"unknown nested field":   func(m map[string]any) { m["items"].([]any)[0].(map[string]any)["state"] = "done" },
		"false evidence omitted": func(m map[string]any) {
			delete(m["items"].([]any)[0].(map[string]any)["evidence"].([]any)[0].(map[string]any), "live_verified")
		},
		"wrong type": func(m map[string]any) { m["v"] = "1" },
	} {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(fixture(t), &m); err != nil {
				t.Fatal(err)
			}
			mutate(m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Parse(raw); err == nil {
				t.Fatal("accepted invalid shape")
			}
		})
	}
	for name, raw := range map[string][]byte{
		"duplicate": []byte(strings.Replace(string(fixture(t)), `"v": 1`, `"v": 1, "v": 1`, 1)),
		"trailing":  append(fixture(t), []byte("{}")...),
		"oversize":  []byte(strings.Repeat(" ", MaxBytes+1)),
		"utf8":      {0xff},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatal("accepted invalid JSON")
			}
		})
	}
}

func TestInvalidMetadata(t *testing.T) {
	for name, change := range map[string]func(*Document){
		"version":            func(d *Document) { d.V = 2 },
		"kind":               func(d *Document) { d.Kind = "arbitrary/v1" },
		"adopted claim":      func(d *Document) { d.Adoption = "adopted" },
		"duplicate node":     func(d *Document) { d.Items = append(d.Items, d.Items[0]) },
		"dangling parent":    func(d *Document) { d.Items[0].Parent = "missing" },
		"parent cycle":       func(d *Document) { d.Items[0].Parent = d.Items[1].Node; d.Items[1].Parent = d.Items[0].Node },
		"dangling anchor":    func(d *Document) { s := "missing"; d.Items[2].Anchor = &s },
		"duplicate order":    func(d *Document) { d.Items[1].Order = d.Items[0].Order },
		"order gap":          func(d *Document) { i := 8; d.Items[0].Order = &i },
		"unknown source":     func(d *Document) { d.Items[0].Estimate.Source = "unknown" },
		"missing acceptance": func(d *Document) { d.Items[0].Acceptance = nil },
		"unverified binding": func(d *Document) {
			d.Items[0].Bindings = []json.RawMessage{json.RawMessage(`{"task_id":"another-step"}`)}
		},
		"live claim":       func(d *Document) { d.Items[0].Evidence[0].LiveVerified = true },
		"confidence":       func(d *Document) { d.Items[0].Estimate.Confidence = "high" },
		"range":            func(d *Document) { d.Items[0].Estimate.WorkDays = []float64{3, 1} },
		"review omitted":   func(d *Document) { d.Items[0].Estimate.ReviewRounds = 0 },
		"missing identity": func(d *Document) { d.Items[0].Responsible.Actor = nil; d.Items[0].Responsible.Seat = nil },
		"self dependency":  func(d *Document) { d.Items[0].Dependencies = []Edge{edge(d, 0, 0)} },
		"block cycle": func(d *Document) {
			d.Items[0].Dependencies = []Edge{edge(d, 0, 1)}
			d.Items[1].Dependencies = []Edge{edge(d, 1, 0)}
		},
		"dangling milestone": func(d *Document) { e := edge(d, 0, 1); e.Milestone = "missing"; d.Items[0].Dependencies = []Edge{e} },
		"required milestone": func(d *Document) { e := edge(d, 0, 1); e.RequiredFor = "missing"; d.Items[0].Dependencies = []Edge{e} },
		"duplicate edge":     func(d *Document) { e := edge(d, 0, 1); d.Items[0].Dependencies = []Edge{e, e} },
	} {
		t.Run(name, func(t *testing.T) {
			d := document(t)
			change(d)
			if err := d.Validate(); err == nil {
				t.Fatal("accepted invalid metadata")
			}
		})
	}
}

func edge(d *Document, from, to int) Edge {
	return Edge{Root: d.Root, Node: d.Items[to].Node, Milestone: d.Items[to].Milestones[0], RequiredFor: d.Items[from].Milestones[0], Relation: "blocks", Reason: "test dependency", Sources: []string{d.Sources[0].ID}}
}

func TestRelatedIsNotBlocking(t *testing.T) {
	d := document(t)
	for i := range d.Items {
		d.Items[i].Dependencies = nil
	}
	a, b := edge(d, 0, 1), edge(d, 1, 0)
	a.Relation, b.Relation = "related", "related"
	d.Items[0].Dependencies, d.Items[1].Dependencies = []Edge{a}, []Edge{b}
	if _, err := Resolve(d.Root, map[string]*Document{d.Root: d}); err != nil {
		t.Fatal(err)
	}
}

func TestCrossRootResolution(t *testing.T) {
	a, b := document(t), document(t)
	b.Root = "R3"
	for i := range a.Items {
		a.Items[i].Dependencies = nil
		b.Items[i].Dependencies = nil
		b.Items[i].Parent = "R3"
	}
	e := edge(a, 0, 0)
	e.Root = "R3"
	a.Items[0].Dependencies = []Edge{e}
	if _, err := Resolve("R4", map[string]*Document{"R4": a}); err == nil {
		t.Fatal("missing external root accepted")
	}
	docs := map[string]*Document{"R4": a, "R3": b}
	if _, err := Resolve("R4", docs); err != nil {
		t.Fatal(err)
	}
	b.Tenant = "business"
	if _, err := Resolve("R4", docs); err == nil {
		t.Fatal("cross-tenant reference accepted")
	}
	b.Tenant = "dev"
	e.Root = "R4"
	b.Items[0].Dependencies = []Edge{e}
	if _, err := Resolve("R4", docs); err == nil {
		t.Fatal("cross-root cycle accepted")
	}
}

func TestDecimalEstimates(t *testing.T) {
	d := document(t)
	d.Items[0].Estimate.WorkDays = []float64{0.5, 1.5}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Items[0].Estimate.WorkDays[0] != 0.5 {
		t.Fatal("decimal rounded")
	}
}
