import type { ClusterPostgresStatusResponse, ClusterServiceReadinessResponse, ClusterSummary, FailingPodView } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { RetrospectivePatternCluster } from '@/api/agentTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import { listFailingMatrixTargets } from '@/lib/control-room/controlRoomOperatePack'
import { isPipelineRunFailed } from '@/lib/delivery/pipelineRunAskPack'
import {
  collectMissionDegradationItems,
  type MissionSnapshot,
} from '@/lib/control-room/missionSignals'
import {
  collectClusterIssues,
  type ClusterIssueCategory,
  type ClusterIssueRow,
} from '@/lib/cluster/collectClusterIssues'

export type RemediationTrack = 'playbook' | 'product' | 'infra' | 'agent-adhoc'

export type FailureTriageRow = {
  id: string
  rank: number
  title: string
  category: ClusterIssueCategory | 'mission' | 'release' | 'matrix'
  severity: 'fail' | 'degraded'
  /** Higher = more urgent for sorting */
  score: number
  occurrences: number
  evidence: string[]
  track: RemediationTrack
  trackReason: string
  suggestedAction: string
  playbookId?: string
  /** Cross-reference from Agent retrospective when available */
  retrospectiveLabel?: string
  retrospectiveOccurrences?: number
}

export type ClusterFailureTriageInput = {
  summary: ClusterSummary
  serviceReadiness?: ClusterServiceReadinessResponse
  postgresStatus?: ClusterPostgresStatusResponse
  missionSnapshot?: MissionSnapshot
  supplyChain?: SupplyChainResponse
  stgSmoke?: StgSmokeResponse
  matrices?: MatrixResponse[]
  retrospectivePatterns?: RetrospectivePatternCluster[]
  topN?: number
}

type PodGroup = {
  key: string
  namespace: string
  reason: string
  pods: FailingPodView[]
}

function severityScore(severity: 'fail' | 'degraded'): number {
  return severity === 'fail' ? 100 : 50
}

function groupFailingPods(pods: FailingPodView[]): PodGroup[] {
  const map = new Map<string, PodGroup>()
  for (const pod of pods) {
    const reason = pod.reason?.trim() || 'Unknown'
    const key = `${pod.namespace}::${reason}`
    const existing = map.get(key)
    if (existing != null) {
      existing.pods.push(pod)
    } else {
      map.set(key, { key, namespace: pod.namespace, reason, pods: [pod] })
    }
  }
  return [...map.values()].sort((a, b) => b.pods.length - a.pods.length)
}

function classifyPodGroup(group: PodGroup): Pick<FailureTriageRow, 'track' | 'trackReason' | 'suggestedAction' | 'playbookId'> {
  const { reason, namespace, pods } = group
  const r = reason.toLowerCase()
  const chronic = pods.length >= 2

  if (r.includes('imagepull') || r.includes('errimagepull')) {
    return {
      track: 'infra',
      trackReason: 'Registry pull failure — not fixed by node recycle',
      suggestedAction: 'Verify registry.cicd reachability, image tag exists, and node registry mirror config',
      playbookId: 'registry-pull-recover',
    }
  }
  // cicd before crashloop/error — Tekton step pods often report reason=Error
  if (namespace === 'cicd') {
    return {
      track: 'playbook',
      trackReason: 'Tekton PipelineRun pod in cicd — delivery pipeline, not K8s node issue',
      suggestedAction:
        'get_pipeline_runs → identify stale Failed run → delete_pipeline_run if terminal; re-run if needed',
      playbookId: 'deliver-stg-recover',
    }
  }
  if (r.includes('crashloop') || r.includes('error')) {
    return chronic
      ? {
          track: 'product',
          trackReason: 'Repeated CrashLoop — likely config/code bug, Agent Fix alone will recur',
          suggestedAction: 'Fix Deployment env/config or GitOps source; add Defects entry; playbook only for logs/events triage',
          playbookId: 'crashloop-triage',
        }
      : {
          track: 'agent-adhoc',
          trackReason: 'Transient or first-seen crash — Agent can collect logs and restart',
          suggestedAction: 'Agent Fix: describe pod events, check logs, rollout restart if dependency recovered',
          playbookId: 'crashloop-triage',
        }
  }
  if (r.includes('createcontainerconfigerror') || r.includes('config')) {
    return {
      track: 'product',
      trackReason: 'Missing ConfigMap/Secret reference — GitOps/manifest fix',
      suggestedAction: 'Restore missing config in repo or fix ArgoCD sync; see deliver-stg / programs path errors',
      playbookId: 'gitops-config-repair',
    }
  }
  if (namespace.startsWith('kube-')) {
    return {
      track: 'infra',
      trackReason: 'Platform namespace pod failure',
      suggestedAction: 'Cluster page → workload logs; restart after root cause',
      playbookId: 'platform-workload-recover',
    }
  }
  return {
    track: 'agent-adhoc',
    trackReason: 'Generic pod failure — needs case-by-case diagnosis',
    suggestedAction: 'Agent Fix with namespace/workload scope; escalate to product if same pod >24h',
    playbookId: 'pod-failure-triage',
  }
}

function classifyClusterIssue(issue: ClusterIssueRow): Pick<FailureTriageRow, 'track' | 'trackReason' | 'suggestedAction' | 'playbookId'> {
  switch (issue.id) {
    case 'elastic-degraded':
      return {
        track: 'playbook',
        trackReason: 'Elastic node degraded — WOL/restart k3s-agent, not replace node',
        suggestedAction: 'POST /cluster/nodes/{name}/wake or SSH restart k3s-agent; uncordon when Ready',
        playbookId: 'elastic-node-recover',
      }
    case 'core-nodes':
      return {
        track: 'playbook',
        trackReason: 'Core node NotReady — drain/evacuate + recover agent on host',
        suggestedAction: 'Cordon → drain workloads → restart k3s on host → uncordon; no auto spare hardware',
        playbookId: 'core-node-recover',
      }
    case 'failing-pods':
      return {
        track: 'agent-adhoc',
        trackReason: 'Aggregate pod failures — see per-namespace groups below',
        suggestedAction: 'Triage grouped pod rows; do not cordon nodes unless NotReady',
        playbookId: 'pod-failure-triage',
      }
    default:
      if (issue.id.startsWith('domain-cicd')) {
        return {
          track: 'playbook',
          trackReason: 'CI/CD domain unhealthy — Tekton/Argo/registry',
          suggestedAction: 'Launch Rocket → STG deliver; fix PipelineRun; verify Gitea sync',
          playbookId: 'cicd-domain-recover',
        }
      }
      if (issue.id.startsWith('domain-')) {
        return {
          track: issue.severity === 'fail' ? 'infra' : 'agent-adhoc',
          trackReason: 'Service domain dependency failure',
          suggestedAction: 'Cluster → service readiness dependencies; fix failing deployment in domain',
          playbookId: 'service-domain-recover',
        }
      }
      if (issue.id.startsWith('postgres-')) {
        return {
          track: 'infra',
          trackReason: 'Data layer — CNPG/MinIO/backup path',
          suggestedAction: 'Cluster postgres panel; CNPG failover or backup config fix',
          playbookId: 'postgres-ha-recover',
        }
      }
      return {
        track: 'agent-adhoc',
        trackReason: 'Uncategorized cluster issue',
        suggestedAction: 'Agent Fix with cluster_issues_full_auto scope',
        playbookId: 'cluster-issues-full-auto',
      }
  }
}

function classifyMissionItem(
  id: string,
  detail: string,
): Pick<FailureTriageRow, 'track' | 'trackReason' | 'suggestedAction' | 'playbookId'> {
  const d = detail.toLowerCase()
  if (id.includes('Release') || id.includes('Supply') || d.includes('deliver')) {
    return {
      track: 'playbook',
      trackReason: 'Delivery pipeline failure — not a node problem',
      suggestedAction: 'Rocket Launch → Launch Rocket; re-run deliver-stg after fixing PipelineRun cause',
      playbookId: 'deliver-stg-recover',
    }
  }
  if (id.includes('IB') || d.includes('ib processes')) {
    return {
      track: 'product',
      trackReason: 'Trade socket edge — config/IB connectivity, persists across node recycle',
      suggestedAction: 'Satellite Bus deep probe; fix IB Gateway/TWS reachability; Trade on K3s lease/HA',
      playbookId: undefined,
    }
  }
  if (id.includes('Self-health') || id.includes('Control')) {
    return {
      track: 'playbook',
      trackReason: 'Platform control plane probe failure',
      suggestedAction: 'Launch Rocket self-health; verify console/API/Argo routes',
      playbookId: 'platform-self-health-recover',
    }
  }
  if (id.includes('gate')) {
    return {
      track: 'playbook',
      trackReason: 'Release gate not passed',
      suggestedAction: 'Run release gate after fixing upstream STG/PROD readiness',
      playbookId: 'release-gate-recover',
    }
  }
  return {
    track: 'agent-adhoc',
    trackReason: 'Mission signal degradation',
    suggestedAction: 'Open matching Launch View (Rocket/Satellite) and Agent Fix with scope hint',
    playbookId: 'cluster-issues-full-auto',
  }
}

function matchRetrospective(
  row: FailureTriageRow,
  patterns: RetrospectivePatternCluster[],
): RetrospectivePatternCluster | undefined {
  const title = row.title.toLowerCase()
  return patterns.find(p => {
    const label = p.label.toLowerCase()
    const ns = p.component.namespace?.toLowerCase() ?? ''
    return (
      label.includes(title.slice(0, 12)) ||
      title.includes(label.slice(0, 12)) ||
      (ns !== '' && row.evidence.some(e => e.toLowerCase().includes(ns)))
    )
  })
}

function buildPodRows(summary: ClusterSummary): FailureTriageRow[] {
  const pods = summary.failing_pod_details ?? []
  if (pods.length === 0) return []

  return groupFailingPods(pods).map((group, index) => {
    const classification = classifyPodGroup(group)
    const sample = group.pods.slice(0, 3).map(p => `${p.namespace}/${p.name}`)
    return {
      id: `pod-group-${group.key}`,
      rank: 0,
      title: `${group.namespace} · ${group.reason} (${group.pods.length} pod${group.pods.length === 1 ? '' : 's'})`,
      category: 'pods',
      severity: 'fail',
      score: severityScore('fail') + group.pods.length * 10 + (index === 0 ? 5 : 0),
      occurrences: group.pods.length,
      evidence: sample,
      ...classification,
    }
  })
}

function buildIssueRows(issues: ClusterIssueRow[]): FailureTriageRow[] {
  return issues
    .filter(i => i.id !== 'failing-pods')
    .map(issue => {
      const classification = classifyClusterIssue(issue)
      return {
        id: issue.id,
        rank: 0,
        title: issue.title,
        category: issue.category,
        severity: issue.severity,
        score: severityScore(issue.severity) + (issue.category === 'nodes' ? 30 : issue.category === 'elastic' ? 25 : 10),
        occurrences: 1,
        evidence: [issue.detail],
        ...classification,
      }
    })
}

function buildMissionRows(snapshot: MissionSnapshot): FailureTriageRow[] {
  return collectMissionDegradationItems(snapshot).map(item => {
    const classification = classifyMissionItem(item.id, item.detail)
    return {
      id: `mission-${item.segment}-${item.id}`,
      rank: 0,
      title: `${item.id} (${item.signal})`,
      category: 'mission',
      severity: item.signal === 'fail' ? 'fail' : 'degraded',
      score: severityScore(item.signal === 'fail' ? 'fail' : 'degraded') + (item.segment === 'rocket' ? 5 : 8),
      occurrences: 1,
      evidence: [item.detail],
      ...classification,
    }
  })
}

function buildReleaseRow(supply?: SupplyChainResponse, stg?: StgSmokeResponse): FailureTriageRow | null {
  if (supply == null) return null
  const last = supply.last_deliver_run
  const smokeTotal = stg?.targets.length ?? 0
  const smokeOk = stg?.targets.filter(t => t.reachability === 'ok').length ?? 0
  const smokeAllOk = smokeTotal > 0 && smokeOk === smokeTotal
  const pipelineFailed = last != null && isPipelineRunFailed(last)

  if (supply.reachability === 'ok' && !pipelineFailed) return null

  const smokeDetail = smokeTotal > 0 ? `STG smoke ${smokeOk}/${smokeTotal}` : ''
  const detail =
    last != null
      ? `Last deliver: ${last.pipeline ?? 'bifrost-deliver-stg'} ${last.status}${last.reason != null ? ` (${last.reason})` : ''}${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`
      : supply.detail || 'Supply chain probe degraded'

  const staleFail = pipelineFailed && smokeAllOk
  return {
    id: 'supply-chain-deliver-stg',
    rank: 0,
    title: staleFail
      ? 'Deliver-stg failed · STG runtime nominal'
      : 'Supply chain / last deliver run',
    category: 'release',
    severity: staleFail ? 'degraded' : supply.reachability === 'fail' || pipelineFailed ? 'fail' : 'degraded',
    score: severityScore(staleFail ? 'degraded' : pipelineFailed ? 'fail' : 'degraded') + 25,
    occurrences: 1,
    evidence: [detail],
    track: 'playbook',
    trackReason: staleFail
      ? 'Pipeline fail with green smoke — GitOps/Tekton, not K8s nodes'
      : 'Tekton deliver pipeline — node recycle does not help',
    suggestedAction: staleFail
      ? 'Playbook deliver-stg-recover: fix rollout/Tekton root cause, re-run deliver-stg (not nodes)'
      : 'Fix PipelineRun failure; identify failing Tekton task; re-run deliver-stg',
    playbookId: 'deliver-stg-recover',
  }
}

function buildMatrixRows(matrices: MatrixResponse[]): FailureTriageRow[] {
  const failing = listFailingMatrixTargets(matrices)
  const byEnv = new Map<string, number>()
  for (const t of failing) {
    byEnv.set(t.environment, (byEnv.get(t.environment) ?? 0) + 1)
  }
  return [...byEnv.entries()].map(([env, count]) => ({
    id: `matrix-${env}`,
    rank: 0,
    title: `Trade matrix · ${env} (${count} failing targets)`,
    category: 'matrix',
    severity: count > 3 ? 'fail' : 'degraded',
    score: severityScore(count > 3 ? 'fail' : 'degraded') + count * 3,
    occurrences: count,
    evidence: failing
      .filter(t => t.environment === env)
      .slice(0, 4)
      .map(t => `${t.id} (${t.reachability})`),
    track: 'agent-adhoc',
    trackReason: 'Reachability probe failures — may be nginx/API/socket not Node',
    suggestedAction: 'Runtime Map drill-down for env; fix specific API/socket targets',
    playbookId: 'matrix-target-triage',
  }))
}

function enrichWithRetrospective(rows: FailureTriageRow[], patterns: RetrospectivePatternCluster[]): FailureTriageRow[] {
  if (patterns.length === 0) return rows
  return rows.map(row => {
    const match = matchRetrospective(row, patterns)
    if (match == null) return row
    const boost = match.occurrences >= 3 ? 15 : match.occurrences >= 2 ? 8 : 0
    let track = row.track
    let trackReason = row.trackReason
    if (match.occurrences >= 3 && row.track === 'agent-adhoc') {
      track = 'product'
      trackReason = `${row.trackReason} · Retrospective: ${match.occurrences} agent jobs — systemic defect`
    }
    return {
      ...row,
      track,
      trackReason,
      score: row.score + boost,
      retrospectiveLabel: match.label,
      retrospectiveOccurrences: match.occurrences,
    }
  })
}

export function buildClusterFailureTriage(input: ClusterFailureTriageInput): FailureTriageRow[] {
  const topN = input.topN ?? 12
  const issues = collectClusterIssues({
    summary: input.summary,
    serviceReadiness: input.serviceReadiness,
    postgresStatus: input.postgresStatus,
  })

  const releaseRow = buildReleaseRow(input.supplyChain, input.stgSmoke)
  let missionRows = input.missionSnapshot != null ? buildMissionRows(input.missionSnapshot) : []
  if (releaseRow != null && releaseRow.title.includes('STG runtime nominal')) {
    missionRows = missionRows.filter(
      row =>
        !(
          row.category === 'mission' &&
          (row.title.toLowerCase().includes('release') || row.title.toLowerCase().includes('supply'))
        ),
    )
  }

  const rows: FailureTriageRow[] = [
    ...buildIssueRows(issues),
    ...buildPodRows(input.summary),
    ...missionRows,
    ...(releaseRow != null ? [releaseRow] : []),
    ...(input.matrices != null ? buildMatrixRows(input.matrices) : []),
  ]

  const enriched = enrichWithRetrospective(rows, input.retrospectivePatterns ?? [])
  const sorted = enriched.sort((a, b) => b.score - a.score).slice(0, topN)
  return sorted.map((row, i) => ({ ...row, rank: i + 1 }))
}

export function formatClusterFailureTriageMarkdown(rows: FailureTriageRow[], generatedAt: string): string {
  const lines: string[] = [
    '# Cluster failure triage — Top N',
    '',
    `Generated: ${generatedAt}`,
    '',
    '| Rank | Severity | Track | Title | Occurrences | Suggested action |',
    '|------|----------|-------|-------|-------------|------------------|',
  ]
  for (const row of rows) {
    const retro =
      row.retrospectiveOccurrences != null
        ? ` _(retro: ${row.retrospectiveLabel}, ${row.retrospectiveOccurrences}×)_`
        : ''
    lines.push(
      `| ${row.rank} | ${row.severity.toUpperCase()} | **${row.track}** | ${row.title}${retro} | ${row.occurrences} | ${row.suggestedAction} |`,
    )
  }
  lines.push('', '## Detail', '')
  for (const row of rows) {
    lines.push(`### ${row.rank}. ${row.title}`)
    lines.push(`- **Track:** ${row.track} — ${row.trackReason}`)
    if (row.playbookId != null) lines.push(`- **Playbook ID (proposed):** \`${row.playbookId}\``)
    lines.push(`- **Evidence:** ${row.evidence.join(' · ')}`)
    lines.push('')
  }
  const byTrack = {
    playbook: rows.filter(r => r.track === 'playbook').length,
    product: rows.filter(r => r.track === 'product').length,
    infra: rows.filter(r => r.track === 'infra').length,
    'agent-adhoc': rows.filter(r => r.track === 'agent-adhoc').length,
  }
  lines.push('## Summary')
  lines.push(`- Playbook candidates: ${byTrack.playbook}`)
  lines.push(`- Product / defect fixes: ${byTrack.product}`)
  lines.push(`- Infra: ${byTrack.infra}`)
  lines.push(`- Agent ad-hoc (until playbook): ${byTrack['agent-adhoc']}`)
  return lines.join('\n')
}
