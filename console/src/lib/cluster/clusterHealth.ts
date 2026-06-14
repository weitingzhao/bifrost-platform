import type { ClusterSummary, Reachability } from '@/api/types'

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
  return {
    reach: summary.reachability,
    label: `Cluster: ${summary.nodes_ready}/${summary.nodes_total} Ready`,
  }
}
