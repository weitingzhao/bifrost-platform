package planview

import "testing"

// Origin: 2026-11-15 domain extraction found store mixed execution counts with
// verified milestone views. A done controller step must not become verified.
func TestExecutionProjectionDoesNotClaimVerification(t *testing.T) {
	nodes := []map[string]any{
		{"plan_node_id": "root", "node_kind": "item"},
		{"plan_node_id": "done", "parent_plan_node_id": "root", "node_kind": "step", "state": "done"},
		{"plan_node_id": "owner", "parent_plan_node_id": "root", "node_kind": "step", "state": "planned", "owner_step": true, "title": "Owner task"},
		{"plan_node_id": "dropped", "parent_plan_node_id": "root", "node_kind": "step", "state": "dropped"},
	}
	graph := Graph{Mainline: []NodeRef{{Root: "R", Node: "first"}, {Root: "R", Node: "second"}}}
	line, next, later := ExecutionSummary(nodes, graph, map[string]string{"first": "done", "second": "root"})
	progress := nodes[0]["execution_progress"].(map[string]any)
	if progress["done"] != 1 || progress["total"] != 2 {
		t.Fatalf("dropped step entered denominator: %#v", progress)
	}
	if line["milestone_status"] != "Unknown" {
		t.Fatalf("execution claimed verification: %#v", line)
	}
	current := line["current"].(map[string]any)
	if current["plan_node_id"] != "root" || current["milestone_status"] != "Unknown" {
		t.Fatalf("wrong unfinished mainline: %#v", current)
	}
	if len(next) != 1 || len(later) != 1 || later[0].(map[string]any)["readiness"] != "Unknown" {
		t.Fatalf("wrong owner/next view: %#v %#v", next, later)
	}
}
