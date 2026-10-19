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
// runtime's stopped and dropped as they are. When is what the board shows
// beside it (FromPlanWithRequest); Diff does not compare it.
type ViewStep struct {
	ID    string `json:"id"`
	T     string `json:"t"`
	State string `json:"state"`
	Owner bool   `json:"owner"`
	Star  bool   `json:"star"`
	When  string `json:"when,omitempty"`
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
// first in ordinal order, each with its done rule and state event.
type planAnswer struct {
	Root  string     `json:"root"`
	Nodes []planNode `json:"nodes"`
}

type planNode struct {
	ID     string          `json:"plan_node_id"`
	Parent *string         `json:"parent_plan_node_id"`
	Kind   string          `json:"node_kind"`
	Label  string          `json:"label"`
	Title  string          `json:"title"`
	State  *string         `json:"state"`
	Owner  bool            `json:"owner_step"`
	Effect bool            `json:"effect_step"`
	Rule   json.RawMessage `json:"done_rule_json"`
	Event  *struct {
		Type string `json:"event_type"`
		At   string `json:"at"`
		Body struct {
			Cause string `json:"cause"`
		} `json:"body"`
	} `json:"state_event"`
}

// FromPlan is the view of a GET /plan?root= answer for the round root. The
// answer must be one round: the root once, each node's id <root>:<label>,
// no id twice, every item under the root and every step under an item before
// it.
func FromPlan(doc []byte, root string) (View, error) {
	return FromPlanWithRequest(doc, root, nil)
}

// FromPlanWithRequest is FromPlan with each step's When: for a step whose
// state is still the one the import gave it (done by its imported manual
// rule, active with cause legacy_imported, or planned since it was created),
// the date its ledger line was marked, from the import request; for any
// other step the time of its last state event. The request is the artifact
// the plan's legacy_imported event names (RequestOf), read once.
func FromPlanWithRequest(doc []byte, root string, req *Request) (View, error) {
	var a planAnswer
	if err := json.Unmarshal(doc, &a); err != nil {
		return View{}, fmt.Errorf("the plan does not read: %w", err)
	}
	if a.Root != root {
		return View{}, fmt.Errorf("the plan is of root %q, not %q", a.Root, root)
	}
	marked := map[string]string{}
	if req != nil {
		for _, n := range req.Nodes {
			if n.Evidence != nil {
				marked[n.ID] = n.Evidence.MarkedAt
			}
		}
	}
	var v View
	at := map[string]int{}
	seen := map[string]bool{}
	roots := 0
	for _, n := range a.Nodes {
		parent := ""
		if n.Parent != nil {
			parent = *n.Parent
		}
		if seen[n.ID] {
			return View{}, fmt.Errorf("node %s is listed twice", n.ID)
		}
		seen[n.ID] = true
		if n.Kind != "round" && n.ID != root+":"+n.Label {
			return View{}, fmt.Errorf("node %s is not %s:%s", n.ID, root, n.Label)
		}
		switch n.Kind {
		case "round":
			if n.ID != root || parent != "" {
				return View{}, fmt.Errorf("round %s inside the plan of %s", n.ID, root)
			}
			roots++
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
				Owner: n.Owner, Star: n.Effect, When: when(n, marked)})
		default:
			return View{}, fmt.Errorf("node %s is a %s, which a round does not hold", n.ID, n.Kind)
		}
	}
	if roots != 1 {
		return View{}, fmt.Errorf("the plan holds its root %d times", roots)
	}
	return v, nil
}

// when is what the board shows beside a step (FromPlanWithRequest).
func when(n planNode, marked map[string]string) string {
	if n.Event == nil {
		return ""
	}
	imported := false
	if m, ok := marked[n.ID]; ok {
		var rule map[string]string
		_ = json.Unmarshal(n.Rule, &rule)
		switch *n.State {
		case StateDone:
			imported = len(rule) == 1 && rule["manual"] == ManualAtom(n.ID) && n.Event.Body.Cause == "done_rule_manual"
		case StateActive:
			imported = n.Event.Body.Cause == "legacy_imported"
		case StatePlanned:
			imported = n.Event.Type == "plan_node_created"
		}
		if imported {
			return m
		}
	}
	return n.Event.At
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

// BoardMissesStars lists the steps got marks ★ where the board's view does
// not, at the same place, and clears those marks in got, so the two can be
// compared on everything else: the board reads a ★ only at the head of a
// title, the import also after 「（补强…）」 (ledger.go).
func BoardMissesStars(board View, got *View) []string {
	var ids []string
	for i := range got.Items {
		for j := range got.Items[i].Steps {
			g := &got.Items[i].Steps[j]
			if i < len(board.Items) && j < len(board.Items[i].Steps) && board.Items[i].Steps[j].ID == g.ID &&
				g.Star && !board.Items[i].Steps[j].Star {
				ids = append(ids, g.ID)
				g.Star = false
			}
		}
	}
	return ids
}

// ImportHead reads one page of GET /events?stream=plan:<root> (events in seq
// order) for the plan's import head: the first legacy_imported event whose
// source names a request artifact (RequestOf). Other events may come before
// it on the stream, a proposal and its decision among them, so a reader
// pages on: when the head is not on this page, next is the page's last seq
// to read after (after_seq), and 0 when the page is empty, which means the
// plan was never imported.
func ImportHead(page []byte) (artifact string, next int64, err error) {
	var events []struct {
		Seq  int64  `json:"seq"`
		Type string `json:"event_type"`
		Body struct {
			Source string `json:"source"`
		} `json:"body_json"`
	}
	if err := json.Unmarshal(page, &events); err != nil {
		return "", 0, fmt.Errorf("the events do not read: %w", err)
	}
	for _, e := range events {
		if id, ok := RequestOf(e.Body.Source); ok && e.Type == "legacy_imported" {
			return id, 0, nil
		}
		next = e.Seq
	}
	return "", next, nil
}
