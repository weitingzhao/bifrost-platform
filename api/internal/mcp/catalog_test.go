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

// stdioMirroredTools must stay registered in mcp/platform/src/index.ts
// (PLATFORM_STDIO_TOOL_NAMES). Catalog Implemented=true alone is not enough.
// Keep in sync with mcp/platform/src/stdioToolNames.ts — Post-QA F3 full set.
var stdioMirroredTools = map[string]bool{
	"platform_mcp_health":                 true,
	"platform_mcp_capabilities":           true,
	"get_connectivity_matrix":             true,
	"verify_payload":                      true,
	"verify_mission_snapshot":             true,
	"list_environments":                   true,
	"get_ops_context":                     true,
	"get_auth_capabilities":               true,
	"get_audit_log":                       true,
	"get_cluster_summary":                 true,
	"get_cluster_nodes":                   true,
	"get_gitops_apps":                     true,
	"get_stack_addons":                    true,
	"get_delivery_pipelines":              true,
	"get_delivery_run_logs":               true,
	"gitops_sync_app":                     true,
	"gitops_rollback_app":                 true,
	"start_pipeline_run":                  true,
	"delete_pipeline_run":                 true,
	"stack_install_addon":                 true,
	"stack_upgrade_addon":                 true,
	"cordon_node":                         true,
	"uncordon_node":                       true,
	"drain_node":                          true,
	"ensure_bifrost_namespaces":           true,
	"rollout_restart_deployment":          true,
	"scale_deployment":                    true,
	"delete_pod":                          true,
	"wake_compute_node":                   true,
	"join_cluster_node":                   true,
	"poweroff_compute_node":               true,
	"ensure_metrics_server":               true,
	"ensure_kube_prometheus_stack":        true,
	"get_session_briefing":                true,
	"list_briefing_session_results":       true,
	"close_briefing_session":              true,
	"prepare_briefing":                    true,
	"update_lane":                         true,
	"get_agent_bridge":                    true,
	"get_hermes_readiness":                true,
	"get_hermes_first_task":               true,
	"get_agent_performance":               true,
	"get_trust_matrix":                    true,
	"get_flight_director_snapshot":        true,
	"get_agent_nightly_report":            true,
	"get_remediation_health":              true,
	"list_remediation_jobs":               true,
	"get_release_state":                   true,
	"get_release_gate":                    true,
	"get_gate_history":                    true,
	"get_stg_smoke":                       true,
	"get_delivery_revisions":              true,
	"run_release_gate":                    true,
	"ensure_kubeconfig_secret":            true,
	"get_program_context":                 true,
	"create_session":                      true,
	"report_phase_progress":               true,
	"submit_post_completion":              true,
	"approve_post_completion_item":        true,
	"reject_post_completion_item":         true,
	"record_no_post_completion_handoff":   true,
	"get_operate_queue":                   true,
	"record_operate_queue_execution":      true,
	"close_operate_queue_item":            true,
	"dismiss_operate_queue_item":          true,
	"get_checklist_signals":               true,
	"get_checklist_kpis":                  true,
	"get_telemetry_overview":              true,
	"get_telemetry_alerts":                true,
	"get_telemetry_targets":               true,
	"report_checklist_signals":            true,
	"sign_tier_b":                         true,
}

func TestParityCriticalToolsImplemented(t *testing.T) {
	byName := map[string]ToolView{}
	for _, tool := range Catalog() {
		byName[tool.Name] = tool
	}
	for name := range stdioMirroredTools {
		tool, ok := byName[name]
		if !ok {
			t.Errorf("missing catalog tool %s", name)
			continue
		}
		if !tool.Implemented {
			t.Errorf("%s: catalog Implemented=false but required for stdio MCP parity", name)
		}
	}
}

func TestCatalogImplementedAllHaveStdioMirror(t *testing.T) {
	for _, tool := range Catalog() {
		if !tool.Implemented {
			continue
		}
		if !stdioMirroredTools[tool.Name] {
			t.Errorf("%s: catalog Implemented=true but missing from stdioMirroredTools (wire mcp/platform or drop Implemented)", tool.Name)
		}
	}
	if len(stdioMirroredTools) != 72 {
		t.Errorf("stdioMirroredTools size=%d want 72 (sync with stdioToolNames.ts)", len(stdioMirroredTools))
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
