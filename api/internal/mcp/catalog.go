package mcp

import (
	"strings"
	"time"
)

const (
	ServerName    = "mcp-server-platform"
	ServerVersion = "0.1.0"
)

// ValidCapabilities is the closed set of functional domains for MCP tools.
var ValidCapabilities = map[string]struct{}{
	"meta":     {},
	"mission":  {},
	"cluster":  {},
	"gitops":   {},
	"delivery": {},
	"stack":    {},
	"release":  {},
	"agent":    {},
}

var ValidFunctions = map[string]struct{}{
	"discover":  {},
	"observe":   {},
	"verify":    {},
	"provision": {},
	"operate":   {},
	"deliver":   {},
	"govern":    {},
	"release":   {},
}

// ValidOwnerRoles are Apollo fleet roles (teams), not Console modules —
// Mission Control is a module surface, so it is intentionally absent here.
var ValidOwnerRoles = map[string]struct{}{
	"rocket":         {},
	"satellite":      {},
	"engineer":       {},
	"ground_systems": {},
	"subcontractors": {},
}

// capabilityFor derives a functional domain from tool name + API route.
// Delivery-batch labels stay in Phase; Capability is what Governance surfaces.
func capabilityFor(name, route string) string {
	if strings.HasPrefix(name, "platform_mcp_") || strings.HasPrefix(route, "/api/v1/mcp") {
		return "meta"
	}
	switch {
	case strings.HasPrefix(route, "/api/v1/matrix"),
		strings.HasPrefix(route, "/api/v1/mission"),
		strings.HasPrefix(route, "/api/v1/environments"),
		strings.HasPrefix(route, "/api/v1/context"),
		strings.HasPrefix(route, "/api/v1/auth"),
		strings.HasPrefix(route, "/api/v1/audit"):
		return "mission"
	case strings.HasPrefix(route, "/api/v1/cluster"):
		return "cluster"
	case strings.HasPrefix(route, "/api/v1/gitops"):
		return "gitops"
	case strings.HasPrefix(route, "/api/v1/delivery"):
		return "delivery"
	case strings.HasPrefix(route, "/api/v1/stack"):
		return "stack"
	case strings.HasPrefix(route, "/api/v1/promote"):
		return "release"
	case strings.HasPrefix(route, "/api/v1/briefing"),
		strings.HasPrefix(route, "/api/v1/lanes"),
		strings.HasPrefix(route, "/api/v1/agent"),
		strings.HasPrefix(route, "/api/v1/hermes"),
		strings.HasPrefix(route, "/api/v1/remediation"),
		strings.HasPrefix(route, "/api/v1/programs"),
		strings.HasPrefix(route, "/api/v1/sessions"),
		strings.HasPrefix(route, "/api/v1/operate"),
		strings.HasPrefix(route, "/api/v1/checklist"),
		strings.HasPrefix(route, "/api/v1/dev-sessions"):
		return "agent"
	default:
		if route == "" {
			return "meta"
		}
		return "mission"
	}
}

// functionFor classifies what the tool does, independently of where it operates.
func functionFor(name, method string) string {
	switch {
	case name == "platform_mcp_capabilities":
		return "discover"
	case strings.HasPrefix(name, "verify_"):
		return "verify"
	case strings.HasPrefix(name, "get_"),
		strings.HasPrefix(name, "list_"),
		name == "platform_mcp_health":
		return "observe"
	case strings.HasPrefix(name, "ensure_"),
		strings.HasPrefix(name, "join_"),
		strings.HasPrefix(name, "wake_"),
		strings.HasPrefix(name, "stack_install_"),
		strings.HasPrefix(name, "stack_upgrade_"):
		return "provision"
	case strings.Contains(name, "release_gate"),
		strings.HasPrefix(name, "sign_tier_"):
		return "release"
	case strings.HasPrefix(name, "gitops_"),
		strings.HasPrefix(name, "start_pipeline_"),
		strings.HasPrefix(name, "delete_pipeline_"):
		return "deliver"
	case strings.HasPrefix(name, "report_"),
		strings.HasPrefix(name, "submit_"),
		strings.HasPrefix(name, "approve_"),
		strings.HasPrefix(name, "reject_"),
		strings.HasPrefix(name, "record_"),
		strings.HasPrefix(name, "close_"),
		strings.HasPrefix(name, "dismiss_"),
		strings.HasPrefix(name, "create_session"),
		strings.HasPrefix(name, "prepare_briefing"),
		strings.HasPrefix(name, "update_lane"),
		strings.HasPrefix(name, "delete_lane"):
		return "govern"
	default:
		if method == "GET" || method == "" {
			return "observe"
		}
		return "operate"
	}
}

// ownerRoleFor identifies the Apollo fleet role (team) primarily served by
// the tool. Mission Control is a Console module, not a role, so mission
// intelligence tools map to the fleet team whose asset they inspect.
// Authorization remains in Role/Level (viewer/operator/admin).
func ownerRoleFor(capability, route string) string {
	switch capability {
	case "meta":
		return "engineer"
	case "mission":
		// Trade-environment probes serve the Satellite team; platform
		// spine/auth/audit serve the Rocket (Ops Platform) team.
		switch {
		case strings.HasPrefix(route, "/api/v1/matrix"),
			strings.HasPrefix(route, "/api/v1/mission"),
			strings.HasPrefix(route, "/api/v1/environments"):
			return "satellite"
		default:
			return "rocket"
		}
	case "cluster":
		return "ground_systems"
	case "gitops", "delivery", "stack", "release":
		return "rocket"
	case "agent":
		return "engineer"
	default:
		return "rocket"
	}
}

func tool(name, desc, level, method, route, role, phase string, implemented bool) ToolView {
	capability := capabilityFor(name, route)
	return ToolView{
		Name:        name,
		Description: desc,
		Level:       level,
		Method:      method,
		Route:       route,
		Role:        role,
		Phase:       phase,
		Capability:  capability,
		Function:    functionFor(name, method),
		OwnerRole:   ownerRoleFor(capability, route),
		Implemented: implemented,
	}
}

func Catalog() []ToolView {
	return []ToolView{
		tool("platform_mcp_health", "MCP server health + version", "read", "", "", "viewer", "P5", true),
		tool("platform_mcp_capabilities", "List MCP tools with permission levels", "read", "GET", "/api/v1/mcp/tools", "viewer", "P5", true),
		tool("get_connectivity_matrix", "Environment connectivity matrix probes", "read", "GET", "/api/v1/matrix", "viewer", "P0", true),
		tool("verify_payload", "Matrix vs cluster datastore classification (NOMINAL/PROBE_DRIFT/DATA_LAYER/HTTP_FAIL)", "read", "GET", "/api/v1/mission/verify-payload", "viewer", "Agent", true),
		tool("verify_mission_snapshot", "Fresh matrix reprobe (trade_dev/stg/prod) + verify_payload + post_fix_verification (call before closing remediation)", "read", "GET", "/api/v1/mission/verify-snapshot", "viewer", "Agent", true),
		tool("list_environments", "Registered environments", "read", "GET", "/api/v1/environments", "viewer", "P0", true),
		tool("get_ops_context", "Spine context (milestones, tracks, blockers)", "read", "GET", "/api/v1/context", "viewer", "P0", true),
		tool("get_auth_capabilities", "Bearer token role and capabilities", "read", "GET", "/api/v1/auth/capabilities", "viewer", "P1", true),
		tool("get_audit_log", "Recent actuation audit records", "read", "GET", "/api/v1/audit", "viewer", "P1", true),
		tool("get_cluster_summary", "Cluster summary probe", "read", "GET", "/api/v1/cluster/", "viewer", "P0", true),
		tool("get_cluster_nodes", "Kubernetes node list", "read", "GET", "/api/v1/cluster/nodes", "viewer", "P0", true),
		tool("get_data_freshness", "CNPG logical DB activity freshness (dev/stg vs prod)", "read", "GET", "/api/v1/cluster/data-freshness", "viewer", "Data", true),
		tool("get_postgres_backup_status", "CNPG Backup CR freshness (completed < 48h)", "read", "GET", "/api/v1/cluster/postgres/backup-status", "viewer", "Data", true),
		tool("trigger_cnpg_backup", "Create on-demand CNPG Backup CR (barmanObjectStore)", "routine", "POST", "/api/v1/cluster/postgres/backup", "operator", "Data", true),
		tool("repair_cnpg_wal_store", "Repair MinIO WAL object store, delete stuck Backup CRs, trigger on-demand backup", "routine", "POST", "/api/v1/cluster/postgres/wal-store/repair", "operator", "Data", true),
		tool("trigger_data_clone", "Clone bifrost_prod → non-prod (admin; confirm:true + confirmation_token=CLONE-FROM-PROD). Default target bifrost_dev only.", "confirm", "POST", "/api/v1/cluster/data-clone", "admin", "Data", true),
		tool("get_data_clone_status", "Poll data-clone job progress", "read", "GET", "/api/v1/cluster/data-clone/{id}", "viewer", "Data", true),
		tool("get_gitops_apps", "Argo CD applications health/sync", "read", "GET", "/api/v1/gitops/apps", "viewer", "P3", true),
		tool("get_stack_addons", "CI/CD stack add-on status", "read", "GET", "/api/v1/stack/addons", "viewer", "P4", true),
		tool("get_delivery_pipelines", "Tekton pipeline catalog", "read", "GET", "/api/v1/delivery/pipelines", "viewer", "P3", true),
		tool("get_delivery_run_logs", "PipelineRun log tail", "read", "GET", "/api/v1/delivery/runs/{id}/logs", "viewer", "P3", true),
		tool("delete_pipeline_run", "Delete terminal Tekton PipelineRun CR + pods (operator; cleans failing_pods leftovers)", "routine", "DELETE", "/api/v1/delivery/runs/{id}", "operator", "P3", true),
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
		tool("ensure_kube_prometheus_stack", "Install kube-prometheus-stack add-on", "confirm", "POST", "/api/v1/cluster/addons/kube-prometheus-stack/ensure", "admin", "P3", true),
		tool("gitops_rollback_app", "Rollback Argo CD app to previous revision", "confirm", "POST", "/api/v1/gitops/apps/{name}/rollback", "admin", "P3", true),
		tool("stack_install_addon", "Install CI/CD stack add-on", "confirm", "POST", "/api/v1/stack/addons/{name}/install", "admin", "P4", true),
		tool("stack_upgrade_addon", "Upgrade/reinstall stack add-on", "confirm", "POST", "/api/v1/stack/addons/{name}/upgrade", "admin", "P4", true),
		tool("get_session_briefing", "Compact/full session briefing pack (track/lane/intent/pack query params)", "read", "GET", "/api/v1/briefing/session-pack", "viewer", "Agent", true),
		tool("list_briefing_session_results", "Recent Agent Desk session close records", "read", "GET", "/api/v1/briefing/session-results", "viewer", "Agent", true),
		tool("close_briefing_session", "Record Agent Desk session close to audit (operator)", "routine", "POST", "/api/v1/briefing/session-results", "operator", "Agent", true),
		tool("prepare_briefing", "Write briefing pack to data/briefing/active-pack.md for Cursor IDE /briefing (Console)", "routine", "POST", "/api/v1/briefing/prepare", "operator", "Console", true),
		tool("update_lane", "Reclassify a Briefing lane (component_line / track_type / track / description)", "routine", "PATCH", "/api/v1/lanes/{id}", "operator", "Agent", true),
		tool("delete_lane", "Delete a Briefing work lane from lanes.yaml", "routine", "DELETE", "/api/v1/lanes/{id}", "operator", "Agent", true),
		tool("get_agent_bridge", "Agent host + MCP bridge status (runner, Hermes slot, platform MCP)", "read", "GET", "/api/v1/agent/bridge", "viewer", "Agent", true),
		tool("get_hermes_readiness", "Hermes gateway + LLM key + platform MCP readiness for first L0 task", "read", "GET", "/api/v1/agent/hermes/readiness", "viewer", "Agent", true),
		tool("get_hermes_first_task", "Canonical Hermes First Task prompt (L0 read-only Mission health pass)", "read", "GET", "/api/v1/agent/hermes/first-task", "viewer", "Agent", true),
		tool("get_hermes_insights", "Analysis Desk Hermes insight ring (newest first; L0 read-only)", "read", "GET", "/api/v1/hermes/insights", "viewer", "Agent", true),
		tool("get_agent_performance", "Flight Director — agent performance KPIs (7d/30d) from remediation JobStore", "read", "GET", "/api/v1/agent/governance/performance", "viewer", "Agent", true),
		tool("get_trust_matrix", "Flight Director — per-task trust & autonomy matrix with earned autonomy hints", "read", "GET", "/api/v1/agent/governance/trust-matrix", "viewer", "Agent", true),
		tool("get_flight_director_snapshot", "Flight Director snapshot — performance + trust + capability map + 24h briefing", "read", "GET", "/api/v1/agent/governance/snapshot", "viewer", "Agent", true),
		tool("get_agent_nightly_report", "Nightly drift scan report from agent host", "read", "GET", "/api/v1/agent/nightly-report", "viewer", "Agent", true),
		tool("get_remediation_health", "Remediation runner health on agent host", "read", "GET", "/api/v1/remediation/health", "viewer", "Agent", true),
		tool("list_remediation_jobs", "Recent agent remediation tasks", "read", "GET", "/api/v1/remediation/", "operator", "Agent", true),
		tool("get_release_state", "Aggregated release state across STG/PROD stages with next-action guidance", "read", "GET", "/api/v1/promote/release-state", "viewer", "P4", true),
		tool("get_release_gate", "Current release gate result, checks, and blockers", "read", "GET", "/api/v1/promote/release-gate", "viewer", "P4", true),
		tool("get_gate_history", "Chronological gate run history for a tier", "read", "GET", "/api/v1/promote/gate-history", "viewer", "P4", true),
		tool("get_stg_smoke", "STG environment HTTP smoke probes", "read", "GET", "/api/v1/delivery/stg/smoke", "viewer", "P4", true),
		tool("get_delivery_revisions", "Available Gitea tags for deploy revision selection", "read", "GET", "/api/v1/delivery/revisions", "viewer", "P4", true),
		tool("run_release_gate", "Run STG or Prod release gate", "confirm", "POST", "/api/v1/promote/release-gate", "admin", "P4", true),
		tool("sign_tier_b", "Record Tier B Owner sign-off", "confirm", "POST", "/api/v1/promote/tier-b/signoff", "admin", "P4", true),
		tool("get_program_context", "Program blueprint + phase sign-off state (archived still fetchable by id; list hides archived unless include_archived=true)", "read", "GET", "/api/v1/programs/{id}", "viewer", "Agent", true),
		tool("get_program_agent_jobs", "Per-program agent job history (active_job + history). Does not switch active program. Not Owner sign-off.", "read", "GET", "/api/v1/programs/{id}/jobs", "viewer", "Agent", true),
		tool("rebind_program_lane", "Rebind a Delivery program to another lane_id (D2: 409 if target lane already has a live program)", "routine", "PATCH", "/api/v1/programs/{id}", "operator", "Agent", true),
		tool("create_session", "Create Session Job archive before phase progress (operator)", "routine", "POST", "/api/v1/sessions", "operator", "Agent", true),
		tool("report_phase_progress", "Report agent phase progress to Delivery Board (session_id required — create_session or Console Copy first; done+verify_cmd requires verify_passed)", "routine", "POST", "/api/v1/programs/{id}/phases/{pid}/progress", "operator", "Agent", true),
		tool("submit_post_completion", "Submit structured handoff draft for Owner review; never auto-approves", "routine", "POST", "/api/v1/programs/{id}/complete", "operator", "Agent", true),
		tool("approve_post_completion_item", "Owner approve structured pending_review handoff into Agent Desk", "confirm", "POST", "/api/v1/programs/post-completion/{itemId}/approve", "admin", "Agent", true),
		tool("reject_post_completion_item", "Owner reject pending handoff without queue injection", "confirm", "POST", "/api/v1/programs/post-completion/{itemId}/reject", "admin", "Agent", true),
		tool("record_no_post_completion_handoff", "Owner record explicit NO HANDOFF assessment", "confirm", "POST", "/api/v1/programs/{id}/post-completion/no-handoff", "admin", "Agent", true),
		tool("get_operate_queue", "Open + recently closed structured Agent Desk handoffs (D11)", "read", "GET", "/api/v1/operate/queue", "viewer", "Agent", true),
		tool("record_operate_queue_execution", "Attach real remediation execution_job_id to open handoff", "routine", "POST", "/api/v1/operate/queue/{id}/execution", "operator", "Agent", true),
		tool("close_operate_queue_item", "Close with completion evidence; linked job must be done and post-fix verification passed", "routine", "POST", "/api/v1/operate/queue/{id}/close", "operator", "Agent", true),
		tool("dismiss_operate_queue_item", "Dismiss stale/resolved handoff with evidence (skips job/post-fix gates)", "routine", "POST", "/api/v1/operate/queue/{id}/dismiss", "operator", "Agent", true),
		tool("get_checklist_signals", "Latest Daily Ops Checklist per-item signals + KPIs", "read", "GET", "/api/v1/checklist/signals", "viewer", "Agent", true),
		tool("report_checklist_signals", "Merge Daily Ops Checklist probe signals (runner daily-ops-checklist-run)", "routine", "POST", "/api/v1/checklist/signals", "operator", "Agent", true),
		tool("get_checklist_kpis", "Checklist quiet-success streak + last-run summary", "read", "GET", "/api/v1/checklist/kpis", "viewer", "Agent", true),
		tool("get_telemetry_overview", "Prometheus telemetry overview snapshot (preset metrics)", "read", "GET", "/api/v1/telemetry/overview", "viewer", "P4", true),
		tool("get_telemetry_alerts", "Prometheus firing and pending alerts", "read", "GET", "/api/v1/telemetry/alerts", "viewer", "P4", true),
		tool("get_telemetry_targets", "Prometheus scrape target health", "read", "GET", "/api/v1/telemetry/targets", "viewer", "P4", true),

		// Dev Sessions tools
		tool("list_dev_sessions", "List sessions for the viewer seat (local bdev in DEV; catalog Deployments in STG/PROD)", "read", "GET", "/api/v1/dev-sessions/", "viewer", "Agent", true),
		tool("restart_dev_session", "Control a session by name: start/stop/restart/clear-logs (bdev locally; K8s scale/rollout in STG/PROD; D10 blocks daemon scale-up)", "routine", "POST", "/api/v1/dev-sessions/{name}/control", "operator", "Agent", true),
		tool("get_dev_session_logs", "Get recent log lines from a session (bdev log file or K8s pod logs)", "read", "GET", "/api/v1/dev-sessions/{name}/logs", "viewer", "Agent", true),
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
		ContractVersion:  "2026-07-21",
		Tools:            tools,
		ImplementedCount: impl,
		GeneratedAt:      time.Now().UTC(),
	}
}
