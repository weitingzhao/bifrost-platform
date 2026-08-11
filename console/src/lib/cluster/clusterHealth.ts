import type { ClusterSummary } from '@/api/clusterTypes'
import type { Reachability } from '@/api/matrixTypes'
import { clusterHealthHint } from '@/lib/cluster/clusterHealthHint'

export function podReachability(phase: string): Reachability {
  switch (phase) {
    case 'Running':
    case 'Succeeded':
      return 'ok'
    case 'Pending':
      return 'degraded'
    default:
      return 'fail'
  }
}

export function summarizeCluster(summary: ClusterSummary | undefined): {
  reach: Reachability
  label: string
} {
  if (!summary) {
    return { reach: 'unknown', label: 'Cluster: loading…' }
  }
  if (summary.reachability !== 'ok') {
    return {
      reach: summary.reachability,
      label:
        summary.reachability === 'fail'
          ? 'Cluster: unreachable'
          : `Cluster: ${summary.detail}`,
    }
  }
  if (summary.failing_pods > 0) {
    return {
      reach: 'degraded',
      label: `Cluster: ${summary.failing_pods} failing pods`,
    }
  }
  const standby = summary.elastic_standby ?? 0
  const label =
    standby > 0
      ? `Cluster: ${summary.nodes_ready}/${summary.nodes_total} core Ready (+${standby} standby)`
      : `Cluster: ${summary.nodes_ready}/${summary.nodes_total} Ready`
  return {
    reach: summary.reachability,
    label,
  }
}

export type ClusterVerdictTagVariant = 'success' | 'warning' | 'danger' | 'neutral'

export type ClusterVerdictDerivation = {
  lamp: Reachability
  tagLabel: string
  tagVariant: ClusterVerdictTagVariant
  summaryLine: string
  /** Compact capacity / failure evidence for Verdict meta (e.g. "2/3 nodes · 1 failing"). */
  evidenceLine: string | null
}

export type DeriveClusterVerdictInput = {
  summary: ClusterSummary | undefined
  unreachable: boolean
  showBootstrapActions: boolean
  summaryFailed: boolean
  isProbing: boolean
  /**
   * Ops plane beyond fleet-only (mission Control/Agent, release, matrix, ranked triage).
   * Same question as Cluster Issues: availability + whether fix is needed.
   */
  opsReach?: Reachability
  opsSummaryLine?: string | null
}

function tagVariantFor(lamp: Reachability): ClusterVerdictTagVariant {
  switch (lamp) {
    case 'ok':
      return 'success'
    case 'degraded':
      return 'warning'
    case 'fail':
      return 'danger'
    default:
      return 'neutral'
  }
}

function buildEvidenceLine(summary: ClusterSummary | undefined): string | null {
  if (summary == null) return null
  const parts: string[] = []
  if (summary.nodes_total > 0) {
    parts.push(`${summary.nodes_ready}/${summary.nodes_total} nodes`)
  }
  if (summary.failing_pods > 0) {
    parts.push(
      `${summary.failing_pods} failing pod${summary.failing_pods === 1 ? '' : 's'}`,
    )
  }
  const elasticDegraded = summary.elastic_degraded ?? 0
  if (elasticDegraded > 0) {
    parts.push(`${elasticDegraded} elastic degraded`)
  }
  return parts.length > 0 ? parts.join(' · ') : null
}

function healthySummaryLine(summary: ClusterSummary): string {
  const hint = clusterHealthHint(summary)
  if (hint != null && hint !== '') return hint
  const summarized = summarizeCluster(summary)
  return summarized.label.replace(/^Cluster:\s*/, '')
}

/**
 * Cluster page Verdict — answers: is the cluster usable, does it need a fix,
 * and how far has health degraded. Fleet checks first; then Ops plane
 * (mission/release/matrix) so READY is never green while Issues still lists work.
 */
export function deriveClusterVerdict(input: DeriveClusterVerdictInput): ClusterVerdictDerivation {
  const {
    summary,
    unreachable,
    showBootstrapActions,
    summaryFailed,
    isProbing,
    opsReach = 'ok',
    opsSummaryLine = null,
  } = input
  const evidenceLine = buildEvidenceLine(summary)

  if (isProbing) {
    return {
      lamp: 'unknown',
      tagLabel: 'PROBING',
      tagVariant: tagVariantFor('unknown'),
      summaryLine: 'Loading cluster snapshot…',
      evidenceLine,
    }
  }

  if (unreachable || summaryFailed) {
    return {
      lamp: 'fail',
      tagLabel: 'UNREACHABLE',
      tagVariant: tagVariantFor('fail'),
      summaryLine:
        summary?.detail != null && summary.detail !== ''
          ? summary.detail
          : 'Cluster unreachable',
      evidenceLine,
    }
  }

  if (summary != null && summary.reachability === 'fail') {
    return {
      lamp: 'fail',
      tagLabel: 'FAIL',
      tagVariant: tagVariantFor('fail'),
      summaryLine:
        summary.detail !== ''
          ? summary.detail
          : (clusterHealthHint(summary) ?? 'Cluster health failed'),
      evidenceLine,
    }
  }

  if (showBootstrapActions) {
    const failing = summary?.failing_pods ?? 0
    const failingNote =
      failing > 0 ? ` · ${failing} failing pod${failing === 1 ? '' : 's'}` : ''
    return {
      lamp: 'degraded',
      tagLabel: 'BOOTSTRAP',
      tagVariant: tagVariantFor('degraded'),
      summaryLine: `One-time bootstrap required (metrics-server or Bifrost namespaces).${failingNote}`,
      evidenceLine,
    }
  }

  if (summary != null && summary.failing_pods > 0) {
    const n = summary.failing_pods
    return {
      lamp: 'fail',
      tagLabel: `${n} FAILING`,
      tagVariant: tagVariantFor('fail'),
      summaryLine: `${n} failing pod${n === 1 ? '' : 's'}`,
      evidenceLine,
    }
  }

  const elasticDegraded = summary?.elastic_degraded ?? 0
  if (
    summary != null &&
    (summary.reachability === 'degraded' ||
      (summary.nodes_total > 0 && summary.nodes_ready < summary.nodes_total) ||
      elasticDegraded > 0)
  ) {
    const nodesPartial =
      summary.nodes_total > 0 && summary.nodes_ready < summary.nodes_total
    const summaryLine =
      summary.reachability === 'degraded'
        ? (clusterHealthHint(summary) ??
          (summary.detail !== '' ? summary.detail : 'Cluster degraded'))
        : nodesPartial
          ? `${summary.nodes_ready}/${summary.nodes_total} core nodes ready`
          : elasticDegraded > 0
            ? `${elasticDegraded} elastic degraded`
            : (summary.detail !== '' ? summary.detail : 'Cluster degraded')
    return {
      lamp: 'degraded',
      tagLabel: 'DEGRADED',
      tagVariant: tagVariantFor('degraded'),
      summaryLine,
      evidenceLine,
    }
  }

  if (summary == null) {
    return {
      lamp: 'unknown',
      tagLabel: 'NO DATA',
      tagVariant: tagVariantFor('unknown'),
      summaryLine: 'Cluster summary unavailable.',
      evidenceLine,
    }
  }

  // Fleet nominal — fold Ops Issues (Agent/Control/release/…) into the same verdict.
  if (opsReach === 'fail') {
    return {
      lamp: 'fail',
      tagLabel: 'NEEDS FIX',
      tagVariant: tagVariantFor('fail'),
      summaryLine:
        opsSummaryLine != null && opsSummaryLine !== ''
          ? opsSummaryLine
          : 'Ops plane failing — AI Agent should remediate',
      evidenceLine,
    }
  }
  if (opsReach === 'degraded' || opsReach === 'unknown') {
    return {
      lamp: 'degraded',
      tagLabel: 'CAUTION',
      tagVariant: tagVariantFor('degraded'),
      summaryLine:
        opsSummaryLine != null && opsSummaryLine !== ''
          ? opsSummaryLine
          : 'Ops probes degraded — AI Agent assessing whether repair is needed',
      evidenceLine,
    }
  }

  return {
    lamp: 'ok',
    tagLabel: 'READY',
    tagVariant: tagVariantFor('ok'),
    summaryLine: healthySummaryLine(summary),
    evidenceLine,
  }
}
