/** Remediation runner scope ids — keep in sync with agentTaskCatalog.ts and agent/remediation prompt routing. */

export const DELIVER_STG_RECOVER_SCOPE = 'deliver-stg-recover'
export const TRADE_RELEASE_FIX_SCOPE = 'trade-release-fix'
export const GITOPS_CONFIG_REPAIR_SCOPE = 'gitops-config-repair'
export const DEFECT_PATTERN_REMEDIATE_SCOPE = 'defect-pattern-remediate'
export const STALE_PIPELINE_TRIAGE_SCOPE = 'stale-pipeline-triage'
export const PLATFORM_SELF_HEALTH_RECOVER_SCOPE = 'platform-self-health-recover'
export const REGISTRY_PULL_RECOVER_SCOPE = 'registry-pull-recover'

/** Maps cluster triage playbookId → remediation scope for one-click Fix. */
export const PLAYBOOK_ID_TO_SCOPE: Record<string, string> = {
  'deliver-stg-recover': DELIVER_STG_RECOVER_SCOPE,
  'gitops-config-repair': GITOPS_CONFIG_REPAIR_SCOPE,
  'platform-self-health-recover': PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
  'registry-pull-recover': REGISTRY_PULL_RECOVER_SCOPE,
  /** cicd / Tekton remnant pods — route to pipeline recover (cleanup + diagnose). */
  'platform-workload-recover': DELIVER_STG_RECOVER_SCOPE,
  'cicd-domain-recover': DELIVER_STG_RECOVER_SCOPE,
}
