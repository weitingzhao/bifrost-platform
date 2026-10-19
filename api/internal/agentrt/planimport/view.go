package planimport

import (
	"encoding/json"
	"fmt"
)

// The plan as the board shows it (roadmap.py's round: items by key and name,
// steps by id, title and state in the board's words), built either from an
// import request or from a GET /plan?root= answer, so the two can be
// compared step by step. The same mapping is what the board uses when it
// reads the plan from the runtime instead of the ledger (CUT §6).

// View is one round.
type View struct {
	Items []ViewItem `json:"items"`
}

// ViewItem is one item and its steps.
type ViewItem struct {
	Key   string     `json:"key"`
	Name  string     `json:"name"`
	Steps []ViewStep `json:"steps"`
}

// ViewStep is one step. State is the board's: done, doing or todo, and the
// runtime's stopped and dropped as they are.
type ViewStep struct {
	ID    string `json:"id"`
	T     string `json:"t"`
	State string `json:"state"`
	Owner bool   `json:"owner"`
	Star  bool   `json:"star"`
}

// BoardState is the board's word for a runtime step state.
func BoardState(state string) string {
	switch state {
	case StateDone:
		return "done"
	case StateActive:
		return "doing"
	case StatePlanned:
		return "todo"
	}
	return state
}

// FromRequest is the view of an import request.
func FromRequest(r Request) View {
	var v View
	at := map[string]int{}
	for _, n := range r.Nodes {
		switch n.Kind {
		case "item":
			at[n.ID] = len(v.Items)
			v.Items = append(v.Items, ViewItem{Key: n.Label, Name: n.Title, Steps: []ViewStep{}})
		case "step":
			i := at[n.Parent]
			v.Items[i].Steps = append(v.Items[i].Steps, ViewStep{ID: n.Label, T: n.Title, State: BoardState(n.State),
				Owner: n.Owner, Star: n.Effect})
		}
	}
	return v
}

// planAnswer is the part of GET /plan?root= a view needs: the nodes, depth
// first in ordinal order.
type planAnswer struct {
	Root  string `json:"root"`
	Nodes []struct {
		ID     string  `json:"plan_node_id"`
		Parent *string `json:"parent_plan_node_id"`
		Kind   string  `json:"node_kind"`
		Label  string  `json:"label"`
		Title  string  `json:"title"`
		State  *string `json:"state"`
		Owner  bool    `json:"owner_step"`
		Effect bool    `json:"effect_step"`
	} `json:"nodes"`
}

// FromPlan is the view of a GET /plan?root= answer for the round root.
func FromPlan(doc []byte, root string) (View, error) {
	var a planAnswer
	if err := json.Unmarshal(doc, &a); err != nil {
		return View{}, fmt.Errorf("the plan does not read: %w", err)
	}
	if a.Root != root {
		return View{}, fmt.Errorf("the plan is of root %q, not %q", a.Root, root)
	}
	var v View
	at := map[string]int{}
	for _, n := range a.Nodes {
		parent := ""
		if n.Parent != nil {
			parent = *n.Parent
		}
		switch n.Kind {
		case "round":
			if n.ID != root {
				return View{}, fmt.Errorf("round %s inside the plan of %s", n.ID, root)
			}
		case "item":
			if parent != root {
				return View{}, fmt.Errorf("item %s sits under %q, not the root", n.ID, parent)
			}
			at[n.ID] = len(v.Items)
			v.Items = append(v.Items, ViewItem{Key: n.Label, Name: n.Title, Steps: []ViewStep{}})
		case "step":
			i, ok := at[parent]
			if !ok || n.State == nil {
				return View{}, fmt.Errorf("step %s sits under %q, which is not an item listed before it", n.ID, parent)
			}
			v.Items[i].Steps = append(v.Items[i].Steps, ViewStep{ID: n.Label, T: n.Title, State: BoardState(*n.State),
				Owner: n.Owner, Star: n.Effect})
		default:
			return View{}, fmt.Errorf("node %s is a %s, which a round does not hold", n.ID, n.Kind)
		}
	}
	return v, nil
}

// Diff lists where got differs from want, step by step, in want's order;
// nothing when they are the same.
func Diff(want, got View) []string {
	var out []string
	have := map[string]ViewItem{}
	for _, it := range got.Items {
		have[it.Key] = it
	}
	seen := map[string]bool{}
	for i, w := range want.Items {
		seen[w.Key] = true
		g, ok := have[w.Key]
		if !ok {
			out = append(out, fmt.Sprintf("item %s (%s) is missing", w.Key, w.Name))
			continue
		}
		if i >= len(got.Items) || got.Items[i].Key != w.Key {
			out = append(out, fmt.Sprintf("item %s is not in place %d", w.Key, i+1))
		}
		if g.Name != w.Name {
			out = append(out, fmt.Sprintf("item %s: name %q, want %q", w.Key, g.Name, w.Name))
		}
		out = append(out, diffSteps(w, g)...)
	}
	for _, g := range got.Items {
		if !seen[g.Key] {
			out = append(out, fmt.Sprintf("item %s (%s) is extra", g.Key, g.Name))
		}
	}
	return out
}

func diffSteps(w, g ViewItem) []string {
	var out []string
	have := map[string]ViewStep{}
	for _, s := range g.Steps {
		have[s.ID] = s
	}
	seen := map[string]bool{}
	for i, ws := range w.Steps {
		seen[ws.ID] = true
		gs, ok := have[ws.ID]
		switch {
		case !ok:
			out = append(out, fmt.Sprintf("step %s (%.40s) is missing", ws.ID, ws.T))
			continue
		case i >= len(g.Steps) || g.Steps[i].ID != ws.ID:
			out = append(out, fmt.Sprintf("step %s is not in place %d", ws.ID, i+1))
		}
		if gs.T != ws.T {
			out = append(out, fmt.Sprintf("step %s: title %q, want %q", ws.ID, gs.T, ws.T))
		}
		if gs.State != ws.State {
			out = append(out, fmt.Sprintf("step %s: state %s, want %s", ws.ID, gs.State, ws.State))
		}
		if gs.Owner != ws.Owner || gs.Star != ws.Star {
			out = append(out, fmt.Sprintf("step %s: owner %v star %v, want owner %v star %v", ws.ID, gs.Owner, gs.Star, ws.Owner, ws.Star))
		}
	}
	for _, gs := range g.Steps {
		if !seen[gs.ID] {
			out = append(out, fmt.Sprintf("step %s (%.40s) is extra", gs.ID, gs.T))
		}
	}
	return out
}
