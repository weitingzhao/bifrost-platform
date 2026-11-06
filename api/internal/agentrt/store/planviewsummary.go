package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// Only existing controller states contribute to execution progress. In
// particular a submitted external run never contributes a done step here.
// A step with work tasks (role not verify or review) counts those tasks, so
// the Owner sees how far its tasks are; its state stays the step's. Items,
// roots and the main line keep counting steps.
func viewExecutionSummary(nodes []map[string]any, tasks []map[string]any, graph planview.Graph, adoption viewAdoption) (map[string]any, []any, []any) {
	byID := map[string]map[string]any{}
	children := map[string][]string{}
	for _, n := range nodes {
		id := n["plan_node_id"].(string)
		byID[id] = n
		if p, ok := n["parent_plan_node_id"].(string); ok {
			children[p] = append(children[p], id)
		}
	}
	var steps func(string) []plan.Step
	steps = func(id string) []plan.Step {
		n := byID[id]
		if n == nil {
			return nil
		}
		if n["node_kind"] == "step" {
			state, _ := n["state"].(string)
			return []plan.Step{{ID: id, State: state}}
		}
		out := []plan.Step{}
		for _, child := range children[id] {
			out = append(out, steps(child)...)
		}
		return out
	}
	work := map[string][]map[string]any{}
	for _, t := range tasks {
		if t["role"] == "verify" || t["role"] == "review" {
			continue
		}
		node, _ := t["plan_node_id"].(string)
		work[node] = append(work[node], t)
	}
	later := []any{}
	for _, n := range nodes {
		id := n["plan_node_id"].(string)
		count := plan.CountSteps(steps(id))
		progress := map[string]any{"state": count.State, "done": count.Done, "total": count.Total, "stopped": count.Stopped, "unit": "steps", "basis": "Existing controller step states; not delivery verification"}
		if ts := work[id]; n["node_kind"] == "step" && len(ts) > 0 {
			done, stopped := 0, 0
			for _, t := range ts {
				switch t["state"] {
				case "accepted":
					done++
				case "rejected", "cancelled":
					stopped++
				default:
					if t["stopped"] != nil {
						stopped++
					}
				}
			}
			progress = map[string]any{"state": count.State, "done": done, "total": len(ts), "stopped": stopped, "unit": "tasks", "basis": "Tasks on this step (checks not counted); the state is the step's"}
		}
		n["execution_progress"] = progress
		if n["owner_step"] == true && n["state"] != "done" && n["state"] != "dropped" {
			later = append(later, map[string]any{"plan_node_id": id, "title": n["title"], "readiness": "Unknown", "reason": "Owner readiness verification requires PV2"})
		}
	}
	path := []any{}
	var current any
	next := []any{}
	for _, ref := range graph.Mainline {
		id := adoption.Nodes[ref.Node]
		count := plan.CountSteps(steps(id))
		entry := map[string]any{"ref": ref, "plan_node_id": id, "execution_state": count.State, "milestone_status": "Unknown"}
		path = append(path, entry)
		if current == nil && count.State != plan.StateDone {
			current = entry
			next = append(next, map[string]any{"plan_node_id": id, "action": "Inspect remaining steps and waiting dependencies", "reason": "First mainline node with unfinished controller steps; milestone verification pending"})
		}
	}
	return map[string]any{"path": path, "current": current, "basis": "Existing controller step states", "milestone_status": "Unknown"}, next, later
}
