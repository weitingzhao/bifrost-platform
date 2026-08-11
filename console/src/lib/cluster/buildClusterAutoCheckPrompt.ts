import type {
  ClusterGovernanceResponse,
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
} from '@/api/clusterTypes'
import type { MatrixResponse, SelfHealthResponse } from '@/api/matrixTypes'
import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import type { RemediationHealthResponse } from '@/api/remediationTypes'
import { scopeForPlaybookId } from '@/lib/agent/playbookAgentPrompts'
import {
  buildClusterFailureTriage,
  formatClusterFailureTriageMarkdown,
  type FailureTriageRow,
} from '@/lib/cluster/clusterFailureTriage'
import {
  collectClusterIssues,
  type ClusterIssueRow,
} from '@/lib/cluster/collectClusterIssues'
import { buildMissionSnapshot, type MissionSnapshot } from '@/lib/control-room/missionSignals'

export type ClusterAutoCheckEvidence = {
  summary: ClusterSummary
  serviceReadiness?: ClusterServiceReadinessResponse
  postgresStatus?: ClusterPostgresStatusResponse
  governance?: ClusterGovernanceResponse
  supplyChain?: SupplyChainResponse
  stgSmoke?: StgSmokeResponse
  selfHealth?: SelfHealthResponse
  runnerHealth?: RemediationHealthResponse
  agentBridge?: AgentBridgeResponse
  matrices?: MatrixResponse[]
  missionSnapshot?: MissionSnapshot
  /** Extra LLM context (cluster dump) appended after triage routing. */
  clusterLlmContext?: string
  topN?: number
}

export type ClusterAutoCheckBundle = {
  fleetIssues: ClusterIssueRow[]
  triageRows: FailureTriageRow[]
  prompt: string
  /** True when fleet issues or ranked triage rows need attention. */
  hasWork: boolean
}

function triageRoutingBlock(rows: FailureTriageRow[]): string {
  if (rows.length === 0) {
    return [
      '## Ops failure triage',
      '',
      'No ranked ops triage rows. Still verify Control/Agent with verify_mission_snapshot.',
      '',
    ].join('\n')
  }

  const lines: string[] = [
    '## Ops failure triage (must cover — not only K8s fleet)',
    '',
    'Treat degraded/unknown mission, release, and matrix rows as **in-scope problems** for this Auto-Check.',
    'Route by playbookId / suggested remediation scope (do not ignore non-fleet rows):',
    '',
  ]
  for (const row of rows) {
    const scope = scopeForPlaybookId(row.playbookId) ?? '(diagnose then pick scoped playbook)'
    lines.push(
      `- #${row.rank} [${row.severity}] ${row.title} · track=${row.track} · playbookId=${row.playbookId ?? '—'} · scope=${scope}`,
    )
    lines.push(`  action: ${row.suggestedAction}`)
  }
  lines.push(
    '',
    '### Routing rules',
    '- Control / Self-health → platform-self-health-recover (Rocket Health probes, platform-api/console/Argo).',
    '- Agent / runner / bridge → operator-plane-remediate (do not recycle K8s nodes for Agent unknown).',
    '- Release / deliver-stg → deliver-stg-recover.',
    '- ImagePull → registry-pull-recover; GitOps ComparisonError → gitops-config-repair.',
    '- Failing pods / NotReady / elastic / data domains → diagnose via cluster tools; safe restart/delete with approval.',
    '- Missing / stale kubeconfig (API unreachable, detail mentions kubeconfig): call **sync_cluster_kubeconfig** / ensure_kubeconfig_secret — do **not** wait for a human Sync button.',
    '- Prefer verify_mission_snapshot after changes; request_operator_approval before destructive actuation.',
    '',
    formatClusterFailureTriageMarkdown(rows, new Date().toISOString()),
    '',
  )
  return lines.join('\n')
}

/** Build Auto-Check prompt that covers fleet issues + Ops Failure Triage (mission/release/matrix). */
export function buildClusterAutoCheckBundle(input: ClusterAutoCheckEvidence): ClusterAutoCheckBundle {
  const fleetIssues = collectClusterIssues({
    summary: input.summary,
    serviceReadiness: input.serviceReadiness,
    postgresStatus: input.postgresStatus,
  })

  const missionSnapshot =
    input.missionSnapshot ??
    buildMissionSnapshot({
      cluster: input.summary,
      supply: input.supplyChain,
      stg: input.stgSmoke,
      self: input.selfHealth,
      runner: input.runnerHealth,
      bridge: input.agentBridge,
      matrices: input.matrices ?? [],
    })

  const triageRows = buildClusterFailureTriage({
    summary: input.summary,
    serviceReadiness: input.serviceReadiness,
    postgresStatus: input.postgresStatus,
    missionSnapshot,
    supplyChain: input.supplyChain,
    stgSmoke: input.stgSmoke,
    matrices: input.matrices,
    topN: input.topN ?? 8,
  })

  const hasWork = fleetIssues.length > 0 || triageRows.length > 0

  const prompt = [
    '# Cluster AI Auto-Check — fleet + ops triage',
    '',
    'Scope: `cluster_issues_full_auto` expanded to cover **Cluster fleet issues** and **Ops failure triage**',
    '(Control, Agent, release, matrix, pods/nodes/data). Unknown/degraded mission signals count as problems.',
    '',
    hasWork
      ? 'Open work detected — diagnose and remediate safely (approval before destructive actions).'
      : 'No open fleet or triage rows — verification pass only; do not speculate destructive fixes.',
    '',
    triageRoutingBlock(triageRows),
    '## Fleet issue list (structured)',
    '',
    fleetIssues.length === 0
      ? '_None — K8s fleet collector empty._'
      : '```json\n' + JSON.stringify(fleetIssues, null, 2) + '\n```',
    '',
    input.clusterLlmContext != null && input.clusterLlmContext.trim() !== ''
      ? ['## Cluster LLM context', '', input.clusterLlmContext.trim(), ''].join('\n')
      : '',
  ]
    .filter(Boolean)
    .join('\n')

  return { fleetIssues, triageRows, prompt, hasWork }
}
