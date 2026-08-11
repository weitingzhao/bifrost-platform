import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { ChevronDown, ChevronRight, Wrench } from 'lucide-react'
import { fetchMatrix, isAllMatrices } from '@/api/core'
import { fetchRetrospectiveReport } from '@/api/agentOps'
import { fetchStgSmoke } from '@/api/promote'
import { fetchSupplyChain } from '@/api/delivery'
import type {
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
} from '@/api/clusterTypes'
import type { RemediationJob } from '@/api/remediationTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import { OpsSection } from '@/components/layout/OpsSection'
import { buildDeliverStgRecoverPrompt } from '@/lib/agent/deliverStgRecoverPrompt'
import { buildPlaybookAgentPrompt, scopeForPlaybookId } from '@/lib/agent/playbookAgentPrompts'
import {
  buildClusterFailureTriage,
  type FailureTriageRow,
  type RemediationTrack,
} from '@/lib/cluster/clusterFailureTriage'
import {
  collectClusterIssues,
  clusterIssuesReachability,
} from '@/lib/cluster/collectClusterIssues'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  formatRemediationJobWhen,
  remediationJobReachability,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'
import type { Reachability } from '@/api/matrixTypes'

const REFETCH_MS = 25_000

function trackVariant(track: RemediationTrack): 'category' | 'warning' | 'danger' | 'success' {
  switch (track) {
    case 'playbook':
      return 'category'
    case 'product':
      return 'warning'
    case 'infra':
      return 'danger'
    default:
      return 'category'
  }
}

function primaryPodReason(reason: string): string {
  const t = reason.trim()
  if (t === '') return '—'
  const head = t.split(':')[0]?.trim() ?? t
  const imageMatch = t.match(/image "([^"]+)"/)
  if (imageMatch != null && head.length <= 48) {
    return `${head} · ${imageMatch[1]}`
  }
  if (head.length < t.length && head.length <= 64) return head
  return t.length > 88 ? `${t.slice(0, 87)}…` : t
}

function TriageRowActions({
  row,
  supply,
  stgSmoke,
  onOpenAgentDesk,
  onOpenDefects,
  onPlaybookFix,
  playbookFixPending,
  canOperate,
}: {
  row: FailureTriageRow
  supply?: SupplyChainResponse
  stgSmoke?: StgSmokeResponse
  onOpenAgentDesk?: (opts: { prefill: string }) => void
  onOpenDefects?: () => void
  onPlaybookFix?: (opts: { scope: string; prompt: string }) => void
  playbookFixPending?: boolean
  canOperate?: boolean
}) {
  const scope = scopeForPlaybookId(row.playbookId)
  if (scope != null && onPlaybookFix != null && canOperate) {
    const prompt =
      row.playbookId === 'deliver-stg-recover'
        ? buildDeliverStgRecoverPrompt({ supply, stgSmoke })
        : buildPlaybookAgentPrompt(row)
    return (
      <Button
        variant="ghost"
        size="xs"
        className="shrink-0"
        disabled={playbookFixPending}
        onClick={() => onPlaybookFix({ scope, prompt })}
        title={`Start ${scope} agent task`}
      >
        <Wrench size={12} className="mr-1" aria-hidden />
        Fix
      </Button>
    )
  }
  if (row.playbookId === 'deliver-stg-recover' && onOpenAgentDesk != null) {
    return (
      <Button
        variant="ghost"
        size="xs"
        className="shrink-0"
        onClick={() =>
          onOpenAgentDesk({
            prefill: buildDeliverStgRecoverPrompt({ supply, stgSmoke }),
          })
        }
        title="Open Agent Desk with deliver-stg-recover playbook"
      >
        <Wrench size={12} className="mr-1" aria-hidden />
        Fix
      </Button>
    )
  }
  if (row.retrospectiveOccurrences != null && row.retrospectiveOccurrences >= 2 && onOpenDefects != null) {
    return (
      <Button variant="ghost" size="xs" onClick={onOpenDefects}>
        Defects
      </Button>
    )
  }
  if (row.playbookId != null && onOpenAgentDesk != null) {
    return (
      <Button
        variant="ghost"
        size="xs"
        onClick={() =>
          onOpenAgentDesk({
            prefill: [
              `Playbook: ${row.playbookId}`,
              '',
              `Issue: ${row.title}`,
              `Track: ${row.track} — ${row.trackReason}`,
              '',
              'Suggested action:',
              row.suggestedAction,
              '',
              'Evidence:',
              ...row.evidence.map(e => `- ${e}`),
            ].join('\n'),
          })
        }
      >
        Agent Fix
      </Button>
    )
  }
  return null
}

function worstReach(a: Reachability, b: Reachability): Reachability {
  if (a === 'fail' || b === 'fail') return 'fail'
  if (a === 'degraded' || b === 'degraded') return 'degraded'
  if (a === 'unknown' || b === 'unknown') return 'unknown'
  return 'ok'
}

export type ClusterOpsIssuesPanelProps = {
  summary: ClusterSummary
  serviceReadiness?: ClusterServiceReadinessResponse
  postgresStatus?: ClusterPostgresStatusResponse
  topN?: number
  onOpenAgentDesk?: (opts: { prefill: string }) => void
  onOpenDefects?: () => void
  onPlaybookFix?: (opts: { scope: string; prompt: string }) => void
  playbookFixPending?: boolean
  onAutoCheck?: () => void
  autoCheckPending?: boolean
  canOperate?: boolean
  onSelectPodNamespace?: (namespace: string) => void
  activeRemediationJob?: RemediationJob | null
  onOpenRemediationSession?: (jobId: string) => void
  /** Notify parent so Cluster Verdict uses the same health grade. */
  onHealthChange?: (health: {
    reach: Reachability
    summaryLine: string
    needsFix: boolean
  }) => void
  /**
   * When true (default), operator sessions auto-start AI Auto-Check once per
   * issue signature — Agent decides whether kubeconfig sync / playbook repair is needed.
   */
  autoAssess?: boolean
  /**
   * Embed under Cluster Verdict (no second OpsSection) — one health composition.
   */
  embedded?: boolean
}

/** Unified Cluster issues: Ops failure triage + fleet detail + one AI Auto-Check. */
export function ClusterOpsIssuesPanel({
  summary,
  serviceReadiness,
  postgresStatus,
  topN = 8,
  onOpenAgentDesk,
  onOpenDefects,
  onPlaybookFix,
  playbookFixPending,
  onAutoCheck,
  autoCheckPending = false,
  canOperate = false,
  onSelectPodNamespace,
  activeRemediationJob = null,
  onOpenRemediationSession,
  onHealthChange,
  autoAssess = true,
  embedded = false,
}: ClusterOpsIssuesPanelProps) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()

  const supplyQ = useQuery({
    queryKey: ['cluster-ops-issues', 'supply'],
    queryFn: fetchSupplyChain,
    refetchInterval: REFETCH_MS,
  })
  const smokeQ = useQuery({
    queryKey: ['cluster-ops-issues', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: REFETCH_MS,
  })
  const matricesQ = useQuery({
    queryKey: ['cluster-ops-issues', 'matrices'],
    queryFn: async () => {
      const data = await fetchMatrix()
      return isAllMatrices(data) ? data.matrices : [data]
    },
    refetchInterval: REFETCH_MS,
    enabled: matrices.length === 0,
  })
  const retroQ = useQuery({
    queryKey: ['cluster-ops-issues', 'retrospective'],
    queryFn: () => fetchRetrospectiveReport(false),
    refetchInterval: 60_000,
  })

  const triageRows = useMemo(
    () =>
      buildClusterFailureTriage({
        summary,
        serviceReadiness,
        postgresStatus,
        missionSnapshot: snapshot,
        supplyChain: supplyQ.data,
        stgSmoke: smokeQ.data,
        matrices: matricesQ.data ?? matrices,
        retrospectivePatterns: retroQ.data?.patterns ?? [],
        topN,
      }),
    [
      summary,
      serviceReadiness,
      postgresStatus,
      snapshot,
      supplyQ.data,
      smokeQ.data,
      matricesQ.data,
      matrices,
      retroQ.data?.patterns,
      topN,
    ],
  )

  const fleetIssues = useMemo(
    () => collectClusterIssues({ summary, serviceReadiness, postgresStatus }),
    [summary, serviceReadiness, postgresStatus],
  )
  const pods = useMemo(() => summary.failing_pod_details ?? [], [summary.failing_pod_details])
  const [podsExpanded, setPodsExpanded] = useState(() => pods.length <= 2)
  const podReasonSummary = useMemo(() => {
    const counts = new Map<string, number>()
    for (const pod of pods) {
      const key = primaryPodReason(pod.reason)
      counts.set(key, (counts.get(key) ?? 0) + 1)
    }
    return [...counts.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 3)
      .map(([reason, count]) => (count > 1 ? `${reason} (×${count})` : reason))
      .join(' · ')
  }, [pods])

  const fleetReach = clusterIssuesReachability(fleetIssues)
  const triageReach: Reachability =
    triageRows.length === 0
      ? 'ok'
      : triageRows.some(r => r.severity === 'fail')
        ? 'fail'
        : 'degraded'
  const overallReach = worstReach(fleetReach, triageReach)
  const allClear = triageRows.length === 0 && fleetIssues.length === 0
  const healthSummaryLine = useMemo(() => {
    if (allClear) return 'Fleet + ops plane clear — no repair needed'
    const top = triageRows[0]
    if (top == null) return `${fleetIssues.length} fleet issue(s) — repair recommended`
    if (triageRows.length === 1) {
      return `${top.title} — ${top.suggestedAction}`
    }
    return `${triageRows.length} ranked issues · top: ${top.title}`
  }, [allClear, triageRows, fleetIssues.length])

  const isLoading = missionLoading || supplyQ.isLoading

  useEffect(() => {
    onHealthChange?.({
      reach: overallReach,
      summaryLine: healthSummaryLine,
      needsFix: !allClear,
    })
  }, [onHealthChange, overallReach, healthSummaryLine, allClear])

  const autoAssessKeyRef = useRef<string | null>(null)
  useEffect(() => {
    if (!autoAssess || !canOperate || onAutoCheck == null) return
    if (isLoading || allClear || autoCheckPending) return
    if (activeRemediationJob?.status === 'running') return
    const key =
      triageRows.length > 0
        ? triageRows.map(r => `${r.id}:${r.severity}`).join('|')
        : `fleet:${fleetIssues.map(i => i.id).join(',')}`
    if (key === '' || autoAssessKeyRef.current === key) return
    autoAssessKeyRef.current = key
    onAutoCheck()
  }, [
    autoAssess,
    canOperate,
    onAutoCheck,
    isLoading,
    allClear,
    autoCheckPending,
    activeRemediationJob?.status,
    triageRows,
    fleetIssues,
  ])

  const sessionActive = activeRemediationJob?.status === 'running'
  const sessionReach = sessionActive ? remediationJobReachability(activeRemediationJob) : 'unknown'
  const sessionStatusLabel = sessionActive ? remediationJobStatusLabel(activeRemediationJob) : ''
  const sessionScopeLabel = sessionActive
    ? remediationScopeShortLabel(activeRemediationJob.scope)
    : ''

  const autoCheckBtn =
    canOperate && onAutoCheck != null ? (
      sessionActive && onOpenRemediationSession != null ? (
        <>
          <div
            className="cluster-remediation-session-chip"
            title={`${sessionScopeLabel} · ${activeRemediationJob.id} · started ${formatRemediationJobWhen(activeRemediationJob.created_at)}`}
          >
            <StatusLamp value={sessionReach} kind="reach" />
            <span className="cluster-remediation-session-chip__title">Debug session</span>
            <span className="cluster-remediation-session-chip__meta">{sessionStatusLabel}</span>
            <span className="cluster-remediation-session-chip__scope">{sessionScopeLabel}</span>
          </div>
          <Button variant="default" size="sm" onClick={() => onOpenRemediationSession(activeRemediationJob.id)}>
            Open in dock
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={autoCheckPending}
            title="Another remediation job is already running"
            onClick={onAutoCheck}
          >
            {autoCheckPending ? 'Starting…' : allClear ? 'New auto-check' : 'New remediate'}
          </Button>
        </>
      ) : (
        <Button variant="default" size="sm" disabled={autoCheckPending} onClick={onAutoCheck}>
          {autoCheckPending ? 'Starting…' : allClear ? 'AI Auto-Check' : 'Auto-Remediate'}
        </Button>
      )
    ) : null

  const defectsBtn =
    onOpenDefects != null ? (
      <Button variant="outline" size="sm" onClick={onOpenDefects}>
        Defects →
      </Button>
    ) : null

  const actions =
    autoCheckBtn != null || defectsBtn != null ? (
      <div className="flex flex-wrap items-center justify-end gap-2">
        {autoCheckBtn}
        {defectsBtn}
      </div>
    ) : undefined

  const body = (
    <>
      {embedded && actions != null ? (
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
            {sessionActive
              ? `Agent assessing / remediating (${sessionStatusLabel.toLowerCase()}). Approve steps in the Operator Dock.`
              : allClear
                ? 'Fleet + ops plane clear — re-verify on demand.'
                : 'Ranked issues — Agent auto-assesses; progress lives in the Operator Dock.'}
          </p>
          {actions}
        </div>
      ) : null}

      {!embedded && sessionActive && onOpenRemediationSession != null && (
        <button
          type="button"
          className={
            allClear
              ? 'cluster-remediation-session-banner cluster-remediation-session-banner--inset'
              : 'cluster-remediation-session-banner'
          }
          onClick={() => onOpenRemediationSession(activeRemediationJob.id)}
        >
          <StatusLamp value={sessionReach} kind="reach" />
          <span className="cluster-remediation-session-banner__text">
            <strong>Agent debug session active</strong>
            <span className="cluster-remediation-session-banner__detail">
              {sessionScopeLabel} · {sessionStatusLabel} · {activeRemediationJob.id.slice(0, 8)}
            </span>
          </span>
          <span className="cluster-remediation-session-banner__cta">Open in dock →</span>
        </button>
      )}

      {embedded && sessionActive && onOpenRemediationSession != null && (
        <button
          type="button"
          className="cluster-remediation-session-banner cluster-remediation-session-banner--inset mb-2"
          onClick={() => onOpenRemediationSession(activeRemediationJob.id)}
        >
          <StatusLamp value={sessionReach} kind="reach" />
          <span className="cluster-remediation-session-banner__text">
            <strong>Agent debug session active</strong>
            <span className="cluster-remediation-session-banner__detail">
              {sessionScopeLabel} · {sessionStatusLabel} · {activeRemediationJob.id.slice(0, 8)}
            </span>
          </span>
          <span className="cluster-remediation-session-banner__cta">Open in dock →</span>
        </button>
      )}

      {isLoading && triageRows.length === 0 && allClear ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
          Ranking fleet, mission, and release signals…
        </p>
      ) : allClear ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No ranked issues. Use{' '}
          <strong className="font-medium text-[var(--foreground)]">AI Auto-Check</strong> to re-verify
          fleet and Control/Agent/release probes autonomously.
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-[5%]">#</DenseTableHead>
              <DenseTableHead className="w-[8%]">Sev</DenseTableHead>
              <DenseTableHead className="w-[10%]">Track</DenseTableHead>
              <DenseTableHead className="w-[10%]">Scope</DenseTableHead>
              <DenseTableHead className="w-[24%]">Issue</DenseTableHead>
              <DenseTableHead className="w-[8%]">Retro</DenseTableHead>
              <DenseTableHead>Suggested action</DenseTableHead>
              <DenseTableHead className="w-[10%]" />
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {triageRows.map(row => (
              <DenseTableRow key={row.id}>
                <DenseTableCell className="font-mono tabular-nums">{row.rank}</DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={row.severity === 'fail' ? 'danger' : 'warning'}>{row.severity}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={trackVariant(row.track)}>{row.track}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant="category">{row.category}</DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-medium">{row.title}</DenseTableCell>
                <DenseTableCell className="font-mono tabular-nums text-muted-foreground">
                  {row.retrospectiveOccurrences != null ? `${row.retrospectiveOccurrences}×` : '—'}
                </DenseTableCell>
                <DenseTableCell
                  className="cluster-issues-cell-clip text-muted-foreground"
                  title={row.suggestedAction}
                >
                  {row.suggestedAction}
                </DenseTableCell>
                <DenseTableCell>
                  <TriageRowActions
                    row={row}
                    supply={supplyQ.data}
                    stgSmoke={smokeQ.data}
                    onOpenAgentDesk={onOpenAgentDesk}
                    onOpenDefects={onOpenDefects}
                    onPlaybookFix={onPlaybookFix}
                    playbookFixPending={playbookFixPending}
                    canOperate={canOperate}
                  />
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}

      {pods.length > 0 && (
        <>
          <button
            type="button"
            className="cluster-issues-pods-toggle"
            onClick={() => setPodsExpanded(open => !open)}
            aria-expanded={podsExpanded}
          >
            {podsExpanded ? (
              <ChevronDown className="size-3.5 shrink-0" aria-hidden />
            ) : (
              <ChevronRight className="size-3.5 shrink-0" aria-hidden />
            )}
            <span className="font-medium">Failing pods ({pods.length})</span>
            {!podsExpanded && podReasonSummary !== '' && (
              <span className="cluster-issues-pods-toggle__summary">{podReasonSummary}</span>
            )}
          </button>
          {podsExpanded && (
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead className="w-[14%]">Namespace</DenseTableHead>
                  <DenseTableHead className="w-[26%]">Pod</DenseTableHead>
                  <DenseTableHead className="w-[8%]">Phase</DenseTableHead>
                  <DenseTableHead>Reason</DenseTableHead>
                  <DenseTableHead className="w-[12%]">Node</DenseTableHead>
                  <DenseTableHead className="w-[6%]">Age</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {pods.map(pod => (
                  <DenseTableRow key={`${pod.namespace}/${pod.name}`}>
                    <DenseTableCell>
                      {onSelectPodNamespace != null ? (
                        <button
                          type="button"
                          className="text-[var(--primary)] underline-offset-2 hover:underline"
                          onClick={() => onSelectPodNamespace(pod.namespace)}
                        >
                          {pod.namespace}
                        </button>
                      ) : (
                        pod.namespace
                      )}
                    </DenseTableCell>
                    <DenseTableCell className="cluster-issues-cell-clip font-mono-tabular" title={pod.name}>
                      {pod.name}
                    </DenseTableCell>
                    <DenseTableCell>
                      <DenseTag variant={pod.phase === 'Running' ? 'success' : 'danger'}>{pod.phase}</DenseTag>
                    </DenseTableCell>
                    <DenseTableCell
                      className="cluster-issues-cell-clip text-[var(--muted-foreground)]"
                      title={pod.reason}
                    >
                      {primaryPodReason(pod.reason)}
                    </DenseTableCell>
                    <DenseTableCell
                      className="cluster-issues-cell-clip font-mono-tabular"
                      title={pod.node ?? undefined}
                    >
                      {pod.node ?? '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular">{pod.age ?? '—'}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          )}
        </>
      )}
    </>
  )

  if (embedded) {
    return (
      <div id="cluster-issues" className="cluster-health-detail scroll-mt-16">
        {body}
      </div>
    )
  }

  if (isLoading && triageRows.length === 0 && allClear) {
    return (
      <OpsSection
        id="cluster-issues"
        className="scroll-mt-16"
        title="Cluster issues"
        description="Ranking fleet, mission, and release signals…"
      >
        <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">Loading issues…</p>
      </OpsSection>
    )
  }

  return (
    <OpsSection
      id="cluster-issues"
      className="scroll-mt-16"
      title="Cluster issues"
      leading={<StatusLamp value={overallReach} kind="reach" />}
      description={
        sessionActive
          ? `Agent assessing / remediating (${sessionStatusLabel.toLowerCase()}). Approve steps in the session — no need to Sync kubeconfig manually.`
          : allClear
            ? 'Same signal as Verdict READY: usable, no repair needed. Agent can still verify on demand.'
            : autoCheckPending || (autoAssess && canOperate)
              ? 'Same signal as Verdict — Agent auto-assesses ranked issues (fleet + Control/Agent/release). Kubeconfig sync is an MCP tool when needed.'
              : 'Same signal as Verdict — ranked issues; run Auto-Remediate so the Agent decides repair (including ensure_kubeconfig_secret).'
      }
      actions={actions}
      bodyPadding={allClear && pods.length === 0 ? 'default' : 'none'}
      overflow="visible"
      bodyClassName={allClear && pods.length === 0 ? undefined : 'ops-section-body--table'}
    >
      {body}
    </OpsSection>
  )
}
