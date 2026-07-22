package mcp

import "testing"

func TestCatalogHasImplementedTools(t *testing.T) {
	resp := ToolsResponseNow()
	if len(resp.Tools) < 10 {
		t.Fatalf("tools: got %d want >= 10", len(resp.Tools))
	}
	if resp.ImplementedCount < 10 {
		t.Fatalf("implemented: got %d", resp.ImplementedCount)
	}
	found := false
	for _, tool := range resp.Tools {
		if tool.Name == "gitops_sync_app" && tool.Implemented {
			found = true
		}
	}
	if !found {
		t.Fatal("gitops_sync_app not in catalog")
	}
}

func TestCatalogCapabilities(t *testing.T) {
	for _, tool := range Catalog() {
		if tool.Capability == "" {
			t.Errorf("%s: empty capability", tool.Name)
		}
		if _, ok := ValidCapabilities[tool.Capability]; !ok {
			t.Errorf("%s: invalid capability %q", tool.Name, tool.Capability)
		}
		if tool.Function == "" {
			t.Errorf("%s: empty function", tool.Name)
		}
		if _, ok := ValidFunctions[tool.Function]; !ok {
			t.Errorf("%s: invalid function %q", tool.Name, tool.Function)
		}
		if tool.OwnerRole == "" {
			t.Errorf("%s: empty owner role", tool.Name)
		}
		if _, ok := ValidOwnerRoles[tool.OwnerRole]; !ok {
			t.Errorf("%s: invalid owner role %q", tool.Name, tool.OwnerRole)
		}
	}
}

func TestCapabilityForSamples(t *testing.T) {
	cases := []struct {
		name, route, want string
	}{
		{"platform_mcp_health", "", "meta"},
		{"platform_mcp_capabilities", "/api/v1/mcp/tools", "meta"},
		{"get_connectivity_matrix", "/api/v1/matrix", "mission"},
		{"get_cluster_nodes", "/api/v1/cluster/nodes", "cluster"},
		{"gitops_sync_app", "/api/v1/gitops/apps/{name}/sync", "gitops"},
		{"start_pipeline_run", "/api/v1/delivery/pipelines/{name}/runs", "delivery"},
		{"stack_install_addon", "/api/v1/stack/addons/{name}/install", "stack"},
		{"run_release_gate", "/api/v1/promote/release-gate", "release"},
		{"get_session_briefing", "/api/v1/briefing/session-pack", "agent"},
		{"get_operate_queue", "/api/v1/operate/queue", "agent"},
	}
	for _, tc := range cases {
		got := capabilityFor(tc.name, tc.route)
		if got != tc.want {
			t.Errorf("capabilityFor(%q, %q)=%q want %q", tc.name, tc.route, got, tc.want)
		}
	}
}

func TestToolClassificationSamples(t *testing.T) {
	cases := []struct {
		name, method, route, function, ownerRole string
	}{
		{"platform_mcp_capabilities", "GET", "/api/v1/mcp/tools", "discover", "engineer"},
		{"verify_mission_snapshot", "GET", "/api/v1/mission/verify-snapshot", "verify", "satellite"},
		{"get_connectivity_matrix", "GET", "/api/v1/matrix", "observe", "satellite"},
		{"get_ops_context", "GET", "/api/v1/context", "observe", "rocket"},
		{"get_audit_log", "GET", "/api/v1/audit", "observe", "rocket"},
		{"ensure_bifrost_namespaces", "POST", "/api/v1/cluster/namespaces/ensure-bifrost", "provision", "ground_systems"},
		{"gitops_sync_app", "POST", "/api/v1/gitops/apps/{name}/sync", "deliver", "rocket"},
		{"run_release_gate", "POST", "/api/v1/promote/release-gate", "release", "rocket"},
		{"get_agent_bridge", "GET", "/api/v1/agent/bridge", "observe", "engineer"},
		{"get_agent_performance", "GET", "/api/v1/agent/governance/performance", "observe", "engineer"},
		{"close_operate_queue_item", "POST", "/api/v1/operate/queue/{id}/close", "govern", "engineer"},
	}
	for _, tc := range cases {
		capability := capabilityFor(tc.name, tc.route)
		if got := functionFor(tc.name, tc.method); got != tc.function {
			t.Errorf("functionFor(%q, %q)=%q want %q", tc.name, tc.method, got, tc.function)
		}
		if got := ownerRoleFor(capability, tc.route); got != tc.ownerRole {
			t.Errorf("ownerRoleFor(%q, %q)=%q want %q", capability, tc.route, got, tc.ownerRole)
		}
	}
}
