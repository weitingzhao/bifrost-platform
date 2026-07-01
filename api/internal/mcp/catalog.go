package mcp

import "time"

const (
	ServerName    = "mcp-server-platform"
	ServerVersion = "0.1.0"
)

func tool(name, desc, level, method, route, role, phase string, implemented bool) ToolView {
	return ToolView{
		Name:        name,
		Description: desc,
		Level:       level,
		Method:      method,
		Route:       route,
		Role:        role,
		Phase:       phase,
		Implemented: implemented,
	}
}

func Catalog() []ToolView {
	return []ToolView{
		tool("platform_mcp_health", "MCP server health + version", "read", "", "", "viewer", "P5", true),
		tool("platform_mcp_capabilities", "List MCP tools with permission levels", "read", "GET", "/api/v1/mcp/tools", "viewer", "P5", true),
		tool("get_connectivity_matrix", "Environment connectivity matrix probes", "read", "GET", "/api/v1/matrix", "viewer", "P0", true),
		tool("list_environments", "Registered environments", "read", "GET", "/api/v1/environments", "viewer", "P0", true),
		tool("get_ops_context", "Spine context (milestones, tracks, blockers)", "read", "GET", "/api/v1/context", "viewer", "P0", true),
		tool("get_auth_capabilities", "Bearer token role and capabilities", "read", "GET", "/api/v1/auth/capabilities", "viewer", "P1", true),
		tool("get_audit_log", "Recent actuation audit records", "read", "GET", "/api/v1/audit", "viewer", "P1", true),
		tool("get_cluster_summary", "Cluster summary probe", "read", "GET", "/api/v1/cluster/", "viewer", "P0", true),
		tool("get_cluster_nodes", "Kubernetes node list", "read", "GET", "/api/v1/cluster/nodes", "viewer", "P0", true),
		tool("get_gitops_apps", "Argo CD applications health/sync", "read", "GET", "/api/v1/gitops/apps", "viewer", "P3", true),
		tool("get_stack_addons", "CI/CD stack add-on status", "read", "GET", "/api/v1/stack/addons", "viewer", "P4", true),
		tool("get_delivery_pipelines", "Tekton pipeline catalog", "read", "GET", "/api/v1/delivery/pipelines", "viewer", "P3", true),
		tool("get_delivery_run_logs", "PipelineRun log tail", "read", "GET", "/api/v1/delivery/runs/{id}/logs", "viewer", "P3", true),
		tool("ensure_bifrost_namespaces", "Create Bifrost namespaces idempotently", "routine", "POST", "/api/v1/cluster/namespaces/ensure-bifrost", "operator", "P1", true),
		tool("rollout_restart_deployment", "Rollout restart a Deployment", "routine", "POST", "/api/v1/cluster/workloads/rollout-restart", "operator", "P1", true),
		tool("scale_deployment", "Scale a Deployment", "routine", "POST", "/api/v1/cluster/workloads/scale", "operator", "P1", true),
		tool("delete_pod", "Delete a Pod", "routine", "DELETE", "/api/v1/cluster/workloads/pods/{ns}/{name}", "operator", "P1", true),
		tool("cordon_node", "Cordon node (no new scheduling)", "routine", "POST", "/api/v1/cluster/nodes/{name}/cordon", "operator", "P2", true),
		tool("uncordon_node", "Uncordon node", "routine", "POST", "/api/v1/cluster/nodes/{name}/uncordon", "operator", "P2", true),
		tool("wake_compute_node", "Wake-on-LAN compute node", "routine", "POST", "/api/v1/cluster/nodes/{name}/wake", "operator", "P1", true),
		tool("gitops_sync_app", "Trigger Argo CD sync to HEAD", "routine", "POST", "/api/v1/gitops/apps/{name}/sync", "operator", "P3", true),
		tool("start_pipeline_run", "Start Tekton PipelineRun", "routine", "POST", "/api/v1/delivery/pipelines/{name}/runs", "operator", "P3", true),
		tool("drain_node", "Drain node workloads", "confirm", "POST", "/api/v1/cluster/nodes/{name}/drain", "admin", "P2", true),
		tool("join_cluster_node", "K3s agent join job", "confirm", "POST", "/api/v1/cluster/nodes/join", "admin", "P2", true),
		tool("poweroff_compute_node", "Drain + power off compute node", "confirm", "POST", "/api/v1/cluster/nodes/{name}/poweroff", "admin", "P1", true),
		tool("ensure_kubeconfig_secret", "Sync kubeconfig and ensure platform-kubeconfig Secret in STG/PROD namespaces", "confirm", "POST", "/api/v1/cluster/kubeconfig-secret/ensure", "admin", "P6", true),
		tool("ensure_metrics_server", "Install metrics-server add-on", "confirm", "POST", "/api/v1/cluster/addons/metrics-server/ensure", "admin", "P1", true),
		tool("gitops_rollback_app", "Rollback Argo CD app to previous revision", "confirm", "POST", "/api/v1/gitops/apps/{name}/rollback", "admin", "P3", true),
		tool("stack_install_addon", "Install CI/CD stack add-on", "confirm", "POST", "/api/v1/stack/addons/{name}/install", "admin", "P4", true),
		tool("stack_upgrade_addon", "Upgrade/reinstall stack add-on", "confirm", "POST", "/api/v1/stack/addons/{name}/upgrade", "admin", "P4", true),
		tool("get_session_briefing", "Compact/full session briefing pack (track/lane/intent/pack query params)", "read", "GET", "/api/v1/briefing/session-pack", "viewer", "Agent", true),
		tool("list_briefing_session_results", "Recent Agent Desk session close records", "read", "GET", "/api/v1/briefing/session-results", "viewer", "Agent", true),
		tool("close_briefing_session", "Record Agent Desk session close to audit (operator)", "routine", "POST", "/api/v1/briefing/session-results", "operator", "Agent", true),
		tool("get_agent_bridge", "Agent host + MCP bridge status (runner, Hermes slot, platform MCP)", "read", "GET", "/api/v1/agent/bridge", "viewer", "Agent", true),
		tool("get_agent_nightly_report", "Nightly drift scan report from agent host", "read", "GET", "/api/v1/agent/nightly-report", "viewer", "Agent", true),
		tool("get_remediation_health", "Remediation runner health on agent host", "read", "GET", "/api/v1/remediation/health", "viewer", "Agent", true),
		tool("list_remediation_jobs", "Recent agent remediation tasks", "read", "GET", "/api/v1/remediation/", "operator", "Agent", true),
		tool("get_release_state", "Aggregated release state across STG/PROD stages with next-action guidance", "read", "GET", "/api/v1/promote/release-state", "viewer", "P4", true),
		tool("get_release_gate", "Current release gate result, checks, and blockers", "read", "GET", "/api/v1/promote/release-gate", "viewer", "P4", true),
		tool("get_gate_history", "Chronological gate run history for a tier", "read", "GET", "/api/v1/promote/gate-history", "viewer", "P4", true),
		tool("get_stg_smoke", "STG environment HTTP smoke probes", "read", "GET", "/api/v1/delivery/stg/smoke", "viewer", "P4", true),
		tool("get_delivery_revisions", "Available Gitea tags for deploy revision selection", "read", "GET", "/api/v1/delivery/revisions", "viewer", "P4", true),
		tool("run_release_gate", "Run STG or Prod release gate", "confirm", "POST", "/api/v1/promote/release-gate", "admin", "P4", true),
		tool("sign_tier_b", "Record Tier B Owner sign-off", "confirm", "POST", "/api/v1/promote/tier-b/signoff", "admin", "P4", false),
	}
}

func ToolsResponseNow() ToolsResponse {
	tools := Catalog()
	impl := 0
	for _, t := range tools {
		if t.Implemented {
			impl++
		}
	}
	return ToolsResponse{
		ServerName:       ServerName,
		ServerVersion:    ServerVersion,
		ContractVersion:  "2026-06-19",
		Tools:            tools,
		ImplementedCount: impl,
		GeneratedAt:      time.Now().UTC(),
	}
}
