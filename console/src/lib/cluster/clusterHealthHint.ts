import type { ClusterSummary } from '@/api/types'

/** Why cluster health (reachability) is not ok — for KPI hints. */
export function clusterHealthHint(summary: ClusterSummary): string | undefined {
  if (summary.reachability === 'ok') {
    return 'All checks pass'
  }
  const parts: string[] = []
  if (summary.failing_pods > 0) {
    parts.push(`${summary.failing_pods} failing pod${summary.failing_pods === 1 ? '' : 's'}`)
  }
  if ((summary.elastic_degraded ?? 0) > 0) {
    parts.push(`${summary.elastic_degraded} elastic degraded`)
  }
  if (summary.nodes_total > 0 && summary.nodes_ready < summary.nodes_total) {
    parts.push(`${summary.nodes_ready}/${summary.nodes_total} core nodes ready`)
  }
  if (parts.length > 0) {
    return parts.join(' · ')
  }
  return summary.detail !== '' ? summary.detail : undefined
}

export function apiReachability(summary: ClusterSummary): ClusterSummary['reachability'] {
  return summary.api_reachability ?? (summary.server_version != null && summary.server_version !== '' ? 'ok' : summary.reachability)
}
