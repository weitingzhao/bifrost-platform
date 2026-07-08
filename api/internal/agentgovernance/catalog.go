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
			ID: "trade-deploy", Scope: "trade-deploy", Label: "Trade · Deploy", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"start_pipeline_run", "get_pipeline_runs", "get_delivery_run_logs", "get_stg_smoke"},
			MissionSignals: []string{"release-gate", "stg-smoke"},
		},
		{
			ID: "deliver-stg-recover", Scope: "deliver-stg-recover", Label: "Trade · Deliver STG Recover", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"get_delivery_run_logs", "get_stg_smoke", "start_pipeline_run", "gitops_sync_app", "get_gitops_apps"},
			MissionSignals: []string{"release-gate", "stg-smoke"},
		},
		{
			ID: "trade-release-fix", Scope: "trade-release-fix", Label: "Trade · Release Fix", Tier: "escalation", DefaultLevel: "L2",
			McpTools: []string{"get_delivery_run_logs", "git_commit", "git_push"},
			MissionSignals: []string{"release-gate"},
		},
		{
			ID: "gitops-config-repair", Scope: "gitops-config-repair", Label: "Platform · GitOps Repair", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"get_gitops_apps", "gitops_sync_app", "start_pipeline_run"},
			MissionSignals: []string{"release-gate"},
		},
		{
			ID: "defect-pattern-remediate", Scope: "defect-pattern-remediate", Label: "Health · Pattern Fix", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"verify_mission_snapshot"},
			MissionSignals: []string{"verify-snapshot"},
		},
		{
			ID: "stale-pipeline-triage", Scope: "stale-pipeline-triage", Label: "Health · Stale Pipeline Check", Tier: "automated", DefaultLevel: "L0",
			McpTools: []string{"get_pipeline_runs", "get_stg_smoke"},
			MissionSignals: []string{"stg-smoke", "release-gate"},
		},
		{
			ID: "platform-self-health-recover", Scope: "platform-self-health-recover", Label: "Platform · Self-health Recover", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"verify_mission_snapshot", "rollout_restart_deployment"},
			MissionSignals: []string{"self-health"},
		},
		{
			ID: "registry-pull-recover", Scope: "registry-pull-recover", Label: "Infra · Registry Pull Recover", Tier: "manual", DefaultLevel: "L1",
			McpTools: []string{"rollout_restart_deployment", "get_cluster_summary"},
			MissionSignals: []string{"cluster"},
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
