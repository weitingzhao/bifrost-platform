import type { FailureTriageRow } from '@/lib/cluster/clusterFailureTriage'
import {
  DELIVER_STG_RECOVER_SCOPE,
  GITOPS_CONFIG_REPAIR_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
  REGISTRY_PULL_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'

export function buildPlaybookAgentPrompt(row: FailureTriageRow): string {
  const header = [
    `Playbook: ${row.playbookId ?? 'unknown'}`,
    '',
    `Issue: ${row.title}`,
    `Track: ${row.track} — ${row.trackReason}`,
    '',
    'Suggested action:',
    row.suggestedAction,
    '',
    'Evidence:',
    ...row.evidence.map(e => `- ${e}`),
  ].join('\n')

  switch (row.playbookId) {
    case 'deliver-stg-recover':
      return header
    case 'platform-workload-recover':
    case 'cicd-domain-recover':
      return [
        header,
        '',
        '## Tekton / cicd namespace workflow',
        '1. get_pipeline_runs — identify failed PipelineRuns in cicd namespace.',
        '2. get_delivery_run_logs for the failing run — root cause diagnosis.',
        '3. If the run is terminal (Failed) and target deployment is healthy: delete_pipeline_run to clean up stale CR + pods (operator approval first).',
        '4. verify_mission_snapshot to confirm failing_pods cleared.',
      ].join('\n')
    case 'gitops-config-repair':
      return [
        header,
        '',
        '## GitOps repair workflow',
        '1. get_gitops_apps — find ComparisonError app.',
        '2. Fix manifest in bifrost-trade-infra or platform overlay.',
        '3. git_commit + mirror sync + gitops_sync_app.',
        '4. Re-run deliver pipeline.',
      ].join('\n')
    case 'platform-self-health-recover':
      return [
        header,
        '',
        '## Self-health workflow',
        '1. verify_mission_snapshot — Control self-health signals.',
        '2. Fix bifrost-platform-prod pods/routes.',
      ].join('\n')
    case 'registry-pull-recover':
      return [
        header,
        '',
        '## Registry workflow',
        '1. Describe ImagePull pods — confirm tag and registry.cicd:30500.',
        '2. Fix image build/push or registry reachability before restart.',
      ].join('\n')
    default:
      return header
  }
}

export function scopeForPlaybookId(playbookId: string | undefined): string | undefined {
  if (playbookId == null) return undefined
  switch (playbookId) {
    case 'deliver-stg-recover':
    case 'platform-workload-recover':
    case 'cicd-domain-recover':
      return DELIVER_STG_RECOVER_SCOPE
    case 'gitops-config-repair':
      return GITOPS_CONFIG_REPAIR_SCOPE
    case 'platform-self-health-recover':
      return PLATFORM_SELF_HEALTH_RECOVER_SCOPE
    case 'registry-pull-recover':
      return REGISTRY_PULL_RECOVER_SCOPE
    default:
      return undefined
  }
}
