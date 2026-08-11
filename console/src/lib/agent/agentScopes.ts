/** Remediation runner scope ids — keep in sync with agentTaskCatalog.ts and agent/remediation prompt routing. */

export const DELIVER_STG_RECOVER_SCOPE = 'deliver-stg-recover'
export const TRADE_RELEASE_FIX_SCOPE = 'trade-release-fix'
export const GITOPS_CONFIG_REPAIR_SCOPE = 'gitops-config-repair'
export const DEFECT_PATTERN_REMEDIATE_SCOPE = 'defect-pattern-remediate'
export const STALE_PIPELINE_TRIAGE_SCOPE = 'stale-pipeline-triage'
export const PLATFORM_SELF_HEALTH_RECOVER_SCOPE = 'platform-self-health-recover'
export const REGISTRY_PULL_RECOVER_SCOPE = 'registry-pull-recover'
/** AI Check on Daily Ops Checklist (prober) — not Operator Plane Fix / fleet cell Fix. */
export const DAILY_OPS_CHECKLIST_RUN_SCOPE = 'daily-ops-checklist-run'
/** Engineer git dirty — propose commit / stash with operator approval. */
export const GIT_DIRTY_REMEDIATE_SCOPE = 'git-dirty-remediate'
/** CNPG barman backup freshness — repair WAL object store + trigger Backup CR when >48h stale. */
export const DATA_LAYER_BACKUP_SCOPE = 'data-layer-backup'
/** Cluster page AI Auto-Check — fleet + ops triage (default K8s SRE prompt + enriched context). */
export const CLUSTER_ISSUES_FULL_AUTO_SCOPE = 'cluster_issues_full_auto'
/** Engineer Operator Plane — agent bridge / remediation runners. */
export const OPERATOR_PLANE_REMEDIATE_SCOPE = 'operator-plane-remediate'

/** Maps cluster triage playbookId → remediation scope for one-click Fix. */
export const PLAYBOOK_ID_TO_SCOPE: Record<string, string> = {
  'deliver-stg-recover': DELIVER_STG_RECOVER_SCOPE,
  'gitops-config-repair': GITOPS_CONFIG_REPAIR_SCOPE,
  'platform-self-health-recover': PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
  'registry-pull-recover': REGISTRY_PULL_RECOVER_SCOPE,
  /** cicd / Tekton remnant pods — route to pipeline recover (cleanup + diagnose). */
  'platform-workload-recover': DELIVER_STG_RECOVER_SCOPE,
  'cicd-domain-recover': DELIVER_STG_RECOVER_SCOPE,
  /** Hyphen playbook id → underscore API scope used by Auto-Check / Agent Fix. */
  'cluster-issues-full-auto': CLUSTER_ISSUES_FULL_AUTO_SCOPE,
  'operator-plane-remediate': OPERATOR_PLANE_REMEDIATE_SCOPE,
}
