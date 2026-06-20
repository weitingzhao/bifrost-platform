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
import type { ClusterSummary } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'

interface ClusterIssuesPanelProps {
  summary: ClusterSummary
  onSelectPodNamespace?: (namespace: string) => void
  canOperate?: boolean
  onAutoRemediate?: () => void
  remediatePending?: boolean
}

interface IssueRow {
  id: string
  category: 'pods' | 'nodes' | 'elastic'
  severity: 'fail' | 'degraded'
  title: string
  detail: string
}

export function collectClusterIssues(summary: ClusterSummary): IssueRow[] {
  return collectIssues(summary)
}

function collectIssues(summary: ClusterSummary): IssueRow[] {
  const issues: IssueRow[] = []

  if (summary.failing_pods > 0) {
    const pods = summary.failing_pod_details ?? []
    const reasons = [...new Set(pods.map(p => p.reason))]
    issues.push({
      id: 'failing-pods',
      category: 'pods',
      severity: 'fail',
      title: `${summary.failing_pods} failing pod${summary.failing_pods === 1 ? '' : 's'}`,
      detail: reasons.length > 0 ? reasons.join(', ') : 'CrashLoopBackOff or Failed phase',
    })
  }

  if (summary.nodes_total > 0 && summary.nodes_ready < summary.nodes_total) {
    issues.push({
      id: 'core-nodes',
      category: 'nodes',
      severity: summary.nodes_ready === 0 ? 'fail' : 'degraded',
      title: `${summary.nodes_ready}/${summary.nodes_total} core nodes ready`,
      detail: 'One or more core (non-elastic) nodes are NotReady',
    })
  }

  const elasticDegraded = summary.elastic_degraded ?? 0
  if (elasticDegraded > 0) {
    issues.push({
      id: 'elastic-degraded',
      category: 'elastic',
      severity: 'degraded',
      title: `${elasticDegraded} elastic node${elasticDegraded === 1 ? '' : 's'} degraded`,
      detail: 'On-demand node needed but not Ready — pending compute workloads or host online but K3s agent down',
    })
  }

  return issues
}

export function ClusterIssuesPanel({
  summary,
  onSelectPodNamespace,
  canOperate = false,
  onAutoRemediate,
  remediatePending = false,
}: ClusterIssuesPanelProps) {
  const issues = collectIssues(summary)

  if (issues.length === 0) {
    return null
  }

  const pods = summary.failing_pod_details ?? []

  return (
    <OpsSection
      title="Cluster issues"
      leading={<StatusLamp value={summary.reachability} kind="reach" />}
      description="Non-green indicators — click a namespace to inspect workloads below."
      actions={
        canOperate && onAutoRemediate != null ? (
          <Button variant="default" size="sm" disabled={remediatePending} onClick={onAutoRemediate}>
            {remediatePending ? 'Starting…' : 'Auto-Remediate'}
          </Button>
        ) : undefined
      }
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
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
              <DenseTableCell className="text-[var(--muted-foreground)]">{issue.detail}</DenseTableCell>
            </DenseTableRow>
          ))}
        </DenseTableBody>
      </DenseDataTable>

      {pods.length > 0 && (
        <>
          <div className="border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-label)] font-medium">
            Failing pods
          </div>
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead className="w-[14%]">Namespace</DenseTableHead>
                <DenseTableHead className="w-[28%]">Pod</DenseTableHead>
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
                  <DenseTableCell className="font-mono-tabular">{pod.name}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant={pod.phase === 'Running' ? 'success' : 'danger'}>{pod.phase}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{pod.reason}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{pod.node ?? '—'}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{pod.age ?? '—'}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </>
      )}
    </OpsSection>
  )
}
