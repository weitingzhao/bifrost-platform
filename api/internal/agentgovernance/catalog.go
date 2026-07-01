package agentgovernance

// TaskDef mirrors console agentTaskCatalog scopes for Flight Director trust matrix.
type TaskDef struct {
	ID           string
	Scope        string
	Label        string
	Tier         string // manual | automated | escalation
	DefaultLevel string // L0 | L1 | L2
	McpTools     []string
	MissionSignals []string
}

func TaskCatalog() []TaskDef {
	return []TaskDef{
		{
			ID: "ops", Scope: "agent-desk", Label: "Ops · Session", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"get_connectivity_matrix", "get_cluster_summary", "get_agent_bridge"},
			MissionSignals: []string{"matrix", "self-health"},
		},
		{
			ID: "release", Scope: "release", Label: "Platform · Release", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"get_release_state", "get_release_gate", "start_pipeline_run"},
			MissionSignals: []string{"release-gate", "stg-smoke"},
		},
		{
			ID: "release-fix", Scope: "release-fix", Label: "Platform · Release Fix", Tier: "escalation", DefaultLevel: "L2",
			McpTools: []string{"get_delivery_run_logs", "gitops_sync_app"},
			MissionSignals: []string{"release-gate"},
		},
		{
			ID: "cluster-auto", Scope: "cluster_issues_full_auto", Label: "Cluster · Remediate", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"rollout_restart_deployment", "delete_pod", "get_cluster_summary"},
			MissionSignals: []string{"matrix", "cluster"},
		},
		{
			ID: "drift-autofix", Scope: "nightly-drift-autofix", Label: "Drift · Fix", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"get_agent_nightly_report"},
			MissionSignals: []string{"drift"},
		},
		{
			ID: "drift-brief", Scope: "nightly-drift-briefing", Label: "Drift · Brief", Tier: "automated", DefaultLevel: "L0",
			McpTools: []string{"get_agent_nightly_report"},
			MissionSignals: []string{"drift"},
		},
		{
			ID: "nightly-health", Scope: "nightly-health-check", Label: "Health · Check", Tier: "automated", DefaultLevel: "L0",
			McpTools: []string{"get_connectivity_matrix", "get_remediation_health"},
			MissionSignals: []string{"matrix", "verify_payload"},
		},
		{
			ID: "post-fix-verification", Scope: "post-fix-verification", Label: "Health · Post-fix", Tier: "manual", DefaultLevel: "L0",
			McpTools: []string{"verify_mission_snapshot", "verify_payload"},
			MissionSignals: []string{"verify-snapshot", "post_fix"},
		},
		{
			ID: "hermes-first-task", Scope: "hermes-first-task", Label: "Hermes · First task", Tier: "manual", DefaultLevel: "L0",
			McpTools: []string{"get_hermes_readiness", "verify_mission_snapshot", "get_connectivity_matrix"},
			MissionSignals: []string{"verify-snapshot", "matrix"},
		},
	}
}

func scopeAliases() map[string]string {
	m := make(map[string]string)
	for _, t := range TaskCatalog() {
		m[t.Scope] = t.Scope
	}
	m["Nightly scheduled health verification"] = "nightly-health-check"
	m[""] = "agent-desk"
	return m
}

func normalizeScope(scope string) string {
	if s, ok := scopeAliases()[scope]; ok {
		return s
	}
	return scope
}

func taskByScope(scope string) (TaskDef, bool) {
	n := normalizeScope(scope)
	for _, t := range TaskCatalog() {
		if t.Scope == n {
			return t, true
		}
	}
	return TaskDef{}, false
}
