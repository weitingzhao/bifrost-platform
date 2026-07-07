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
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useMemo, useState } from 'react'
import type {
  ClusterPostgresStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  RemediationJob,
} from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  collectClusterIssues,
  clusterIssuesReachability,
} from '@/lib/cluster/collectClusterIssues'
import {
  formatRemediationJobWhen,
  remediationJobReachability,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'

interface ClusterIssuesPanelProps {
  summary: ClusterSummary
  serviceReadiness?: ClusterServiceReadinessResponse
  postgresStatus?: ClusterPostgresStatusResponse
  onSelectPodNamespace?: (namespace: string) => void
  canOperate?: boolean
  onAutoRemediate?: () => void
  remediatePending?: boolean
  activeRemediationJob?: RemediationJob | null
  onOpenRemediationSession?: (jobId: string) => void
}

export { collectClusterIssues, type ClusterIssueRow } from '@/lib/cluster/collectClusterIssues'

/** One-line summary for dense tables; full text stays in title/tooltip. */
function compactIssueText(text: string, max = 96): string {
  const t = text.trim()
  if (t.length <= max) return t
  return `${t.slice(0, max - 1)}…`
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
  return compactIssueText(t, 88)
}

export function ClusterIssuesPanel({
  summary,
  serviceReadiness,
  postgresStatus,
  onSelectPodNamespace,
  canOperate = false,
  onAutoRemediate,
  remediatePending = false,
  activeRemediationJob = null,
  onOpenRemediationSession,
}: ClusterIssuesPanelProps) {
  const issues = collectClusterIssues({
    summary,
    serviceReadiness,
    postgresStatus,
  })
  const pods = summary.failing_pod_details ?? []
  const healthy = issues.length === 0
  const issueReach = clusterIssuesReachability(issues)
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
  const sessionActive = activeRemediationJob?.status === 'running'
  const sessionReach = sessionActive ? remediationJobReachability(activeRemediationJob) : 'unknown'
  const sessionStatusLabel = sessionActive ? remediationJobStatusLabel(activeRemediationJob) : ''
  const sessionScopeLabel = sessionActive
    ? remediationScopeShortLabel(activeRemediationJob.scope)
    : ''

  const remediateActions =
    canOperate && onAutoRemediate != null
      ? sessionActive && onOpenRemediationSession != null
        ? (
            <div className="flex flex-wrap items-center justify-end gap-2">
              <div
                className="cluster-remediation-session-chip"
                title={`${sessionScopeLabel} · ${activeRemediationJob.id} · started ${formatRemediationJobWhen(activeRemediationJob.created_at)}`}
              >
                <StatusLamp value={sessionReach} kind="reach" />
                <span className="cluster-remediation-session-chip__title">Debug session</span>
                <span className="cluster-remediation-session-chip__meta">{sessionStatusLabel}</span>
                <span className="cluster-remediation-session-chip__scope">{sessionScopeLabel}</span>
              </div>
              <Button
                variant="default"
                size="sm"
                onClick={() => onOpenRemediationSession(activeRemediationJob.id)}
              >
                Open session
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={remediatePending}
                title="Another remediation job is already running"
                onClick={onAutoRemediate}
              >
                {remediatePending ? 'Starting…' : healthy ? 'New auto-check' : 'New remediate'}
              </Button>
            </div>
          )
        : (
            <Button variant="default" size="sm" disabled={remediatePending} onClick={onAutoRemediate}>
              {remediatePending ? 'Starting…' : healthy ? 'AI Auto-Check' : 'Auto-Remediate'}
            </Button>
          )
      : undefined

  return (
    <OpsSection
      title="Cluster issues"
      leading={<StatusLamp value={healthy ? 'ok' : issueReach} kind="reach" />}
      description={
        sessionActive
          ? `Agent debug session in progress (${sessionStatusLabel.toLowerCase()}). Open it to approve steps or read activity — do not start a duplicate unless you intend a parallel run.`
          : healthy
            ? 'No failing pods, node readiness, elastic degradation, or data-layer gaps detected. Run AI Auto-Check to confirm.'
            : 'Pods, nodes, elastic hosts, and data-layer readiness (PostgreSQL, MinIO, Redis, …). Click a namespace to inspect workloads below.'
      }
      actions={remediateActions}
      bodyPadding={healthy ? 'default' : 'none'}
      overflow="visible"
      bodyClassName={healthy ? undefined : 'ops-section-body--table'}
    >
      {healthy ? (
        <>
          {sessionActive && onOpenRemediationSession != null && (
            <button
              type="button"
              className="cluster-remediation-session-banner cluster-remediation-session-banner--inset"
              onClick={() => onOpenRemediationSession(activeRemediationJob.id)}
            >
              <StatusLamp value={sessionReach} kind="reach" />
              <span className="cluster-remediation-session-banner__text">
                <strong>Agent debug session active</strong>
                <span className="cluster-remediation-session-banner__detail">
                  {sessionScopeLabel} · {sessionStatusLabel} · {activeRemediationJob.id.slice(0, 8)}
                </span>
              </span>
              <span className="cluster-remediation-session-banner__cta">Open session →</span>
            </button>
          )}
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            All monitored checks are green. Use{' '}
            <strong className="font-medium text-[var(--foreground)]">AI Auto-Check</strong> above to run an
            autonomous health pass — the agent will confirm status or remediate if it finds something the summary
            missed.
          </p>
        </>
      ) : (
        <>
          {sessionActive && onOpenRemediationSession != null && (
            <button
              type="button"
              className="cluster-remediation-session-banner"
              onClick={() => onOpenRemediationSession(activeRemediationJob.id)}
            >
              <StatusLamp value={sessionReach} kind="reach" />
              <span className="cluster-remediation-session-banner__text">
                <strong>Agent debug session active</strong>
                <span className="cluster-remediation-session-banner__detail">
                  {sessionScopeLabel} · {sessionStatusLabel} · {activeRemediationJob.id.slice(0, 8)} ·{' '}
                  {formatRemediationJobWhen(activeRemediationJob.created_at)}
                </span>
              </span>
              <span className="cluster-remediation-session-banner__cta">Open session →</span>
            </button>
          )}
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="w-[6%]" />
                <DenseTableHead className="w-[12%]">Category</DenseTableHead>
                <DenseTableHead className="w-[30%]">Issue</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {issues.map(issue => (
                <DenseTableRow key={issue.id}>
                  <DenseTableCell>
                    <StatusLamp value={issue.severity} kind="reach" />
                  </DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={issue.severity === 'fail' ? 'danger' : 'warning'}>{issue.category}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-medium">{issue.title}</DenseTableCell>
                  <DenseTableCell
                    className="cluster-issues-cell-clip text-[var(--muted-foreground)]"
                    title={issue.detail}
                  >
                    {compactIssueText(issue.detail)}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>

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
                      <DenseTableCell className="cluster-issues-cell-clip font-mono-tabular" title={pod.node ?? undefined}>
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
      )}
    </OpsSection>
  )
}
