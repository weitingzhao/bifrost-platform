package store

import (
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planview"
)

// Only existing controller states contribute to execution progress. In
// particular a submitted external run never contributes a done step here.
func viewExecutionSummary(nodes []map[string]any, graph planview.Graph, adoption viewAdoption) (map[string]any, []any, []any) {
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
	later := []any{}
	for _, n := range nodes {
		id := n["plan_node_id"].(string)
		count := plan.CountSteps(steps(id))
		n["execution_progress"] = map[string]any{"state": count.State, "done": count.Done, "total": count.Total, "stopped": count.Stopped, "basis": "Existing controller step states; not delivery verification"}
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
