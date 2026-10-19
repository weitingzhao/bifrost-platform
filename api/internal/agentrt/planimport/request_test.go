package planimport_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
)

// Each change breaks one rule of the request's shape; Validate names it.
func TestValidateRefusals(t *testing.T) {
	stepAt := func(r *planimport.Request, id string) *planimport.Node {
		for i := range r.Nodes {
			if r.Nodes[i].ID == id {
				return &r.Nodes[i]
			}
		}
		t.Fatalf("no node %s", id)
		return nil
	}
	for name, c := range map[string]struct {
		change func(*planimport.Request)
		want   string
	}{
		"format":            {func(r *planimport.Request) { r.Format = "v0" }, "format is"},
		"digest":            {func(r *planimport.Request) { r.Source.SHA256 = "abc" }, "source.sha256"},
		"commit":            {func(r *planimport.Request) { r.Source.Commit = "1234567" }, "40-character commit"},
		"root kind":         {func(r *planimport.Request) { r.Root.Kind = "item" }, "root is a round"},
		"root id":           {func(r *planimport.Request) { r.Root.ID = "R3" }, "root id is R2"},
		"root ordinal":      {func(r *planimport.Request) { r.Root.Ordinal = 0 }, "root ordinal is the round number"},
		"root state":        {func(r *planimport.Request) { r.Root.State = "done" }, "root has no parent"},
		"id not from label": {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").ID = "R2:x" }, "id is R2:2.0-1"},
		"label renumbered": {func(r *planimport.Request) {
			n := stepAt(r, "R2:2.0-2")
			n.Label, n.ID = "2.0-7", "R2:2.0-7"
		}, "a step's label is 2.0-2"},
		"step out of order":   {func(r *planimport.Request) { stepAt(r, "R2:2.0-2").Ordinal = 5 }, "steps are numbered 0, 1"},
		"item out of order":   {func(r *planimport.Request) { stepAt(r, "R2:2.1").Ordinal = 4 }, "items are numbered 0, 1"},
		"step under the root": {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").Parent = "R2" }, "a step sits under an item listed before it"},
		"item under an item":  {func(r *planimport.Request) { stepAt(r, "R2:2.1").Parent = "R2:2.0" }, "an item sits under the root"},
		"stopped on import":   {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").State = "stopped" }, "planned, active or done"},
		"step kind":           {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").StepKind = "chat" }, "step kind is one of"},
		"item with a state":   {func(r *planimport.Request) { stepAt(r, "R2:2.1").State = "done" }, "an item has no step kind"},
		"no evidence":         {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").Evidence = nil }, "carries the line"},
		"evidence elsewhere":  {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").Evidence.LineNo = 1 }, "outside the source's lines"},
		"control in a title":  {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").Title = "a\nb" }, "control character"},
		"work id":             {func(r *planimport.Request) { stepAt(r, "R2:2.0-1").WorkID = "TD-1" }, "work id is W-<n>"},
		"kind under the root": {func(r *planimport.Request) { stepAt(r, "R2:2.1").Kind = "arch_row" }, "an item or a step"},
		"too many": {func(r *planimport.Request) {
			for len(r.Nodes) <= planimport.MaxNodes {
				r.Nodes = append(r.Nodes, r.Nodes[0])
			}
		}, "a request holds 1 to 500 nodes"},
	} {
		t.Run(name, func(t *testing.T) {
			r := fixture(t)
			c.change(&r)
			err := r.Validate()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v; want it to contain %q", err, c.want)
			}
		})
	}
}

// A GET /plan answer read back as a view; the differences a reconciliation
// must see are each named.
func TestFromPlanAndDiff(t *testing.T) {
	r := fixture(t)
	var nodes []map[string]any
	add := func(n planimport.Node) {
		row := map[string]any{"plan_node_id": n.ID, "node_kind": n.Kind, "label": n.Label, "title": n.Title,
			"owner_step": n.Owner, "effect_step": n.Effect}
		if n.Parent != "" {
			row["parent_plan_node_id"] = n.Parent
		}
		if n.State != "" {
			row["state"] = n.State
		}
		nodes = append(nodes, row)
	}
	add(r.Root)
	for _, n := range r.Nodes {
		add(n)
	}
	answer := func() []byte {
		doc, err := json.Marshal(map[string]any{"root": "R2", "nodes": nodes})
		ok(t, err)
		return doc
	}
	want := planimport.FromRequest(r)
	got, err := planimport.FromPlan(answer(), "R2")
	ok(t, err)
	if d := planimport.Diff(want, got); len(d) != 0 {
		t.Fatalf("the plan written from the request differs from it: %v", d)
	}
	if _, rerr := planimport.FromPlan(answer(), "R3"); rerr == nil {
		t.Fatal("a plan of R2 read as R3")
	}
	// A step done in the runtime, a retitled one, one more step, one gone.
	nodes[2]["state"] = "planned"
	nodes[3]["title"] = "renamed"
	extra := map[string]any{"plan_node_id": "R2:2.0-4", "parent_plan_node_id": "R2:2.0", "node_kind": "step", "label": "2.0-4",
		"title": "new", "state": "planned", "owner_step": false, "effect_step": false}
	nodes = append(nodes[:4], append([]map[string]any{extra}, nodes[5:]...)...)
	got, err = planimport.FromPlan(answer(), "R2")
	ok(t, err)
	d := strings.Join(planimport.Diff(want, got), "\n")
	for _, w := range []string{"step 2.0-1: state todo, want done", "step 2.0-2: title \"renamed\"", "step 2.0-3 (", "is missing", "step 2.0-4 (new) is extra"} {
		if !strings.Contains(d, w) {
			t.Errorf("diff does not say %q:\n%s", w, d)
		}
	}
	// A step under a node that is not an item is not a plan of rounds.
	nodes = append(nodes, map[string]any{"plan_node_id": "R2:x", "parent_plan_node_id": "R2", "node_kind": "step", "label": "x",
		"title": "x", "state": "planned"})
	if _, err := planimport.FromPlan(answer(), "R2"); err == nil {
		t.Fatal("a step under the root was read")
	}
}
