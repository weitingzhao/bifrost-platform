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
 * Cluster page Verdict — priority order is locked (see Rocket R5 plan).
 * lamp is Reachability-compatible with OpsVerdictLamp.
 */
export function deriveClusterVerdict(input: DeriveClusterVerdictInput): ClusterVerdictDerivation {
  const { summary, unreachable, showBootstrapActions, summaryFailed, isProbing } = input
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

  return {
    lamp: 'ok',
    tagLabel: 'READY',
    tagVariant: tagVariantFor('ok'),
    summaryLine: healthySummaryLine(summary),
    evidenceLine,
  }
}
