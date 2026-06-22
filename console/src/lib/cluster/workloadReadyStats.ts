import type { ClusterWorkload } from '@/api/types'
import { allowedNamespaceNames, type NsFilterType } from '@/lib/cluster/namespaceCatalog'
import { groupWorkloadsByDeployment } from '@/lib/cluster/workloadTree'

export type ReadyCount = { actual: number; planned: number }

export type ReadyTone = 'ok' | 'warn' | 'fail' | 'idle'

export type NamespaceReadyStats = {
  deploymentReady: ReadyCount
  standaloneReady: ReadyCount
  deploymentOk: number
  deploymentFailed: number
  deploymentTotal: number
}

export function parseReady(ready: string): ReadyCount {
  const match = ready.match(/^(\d+)\/(\d+)$/)
  if (match == null) return { actual: 0, planned: 0 }
  return { actual: Number(match[1]), planned: Number(match[2]) }
}

export function mergeReadyCounts(counts: ReadyCount[]): ReadyCount {
  return counts.reduce(
    (acc, count) => ({
      actual: acc.actual + count.actual,
      planned: acc.planned + count.planned,
    }),
    { actual: 0, planned: 0 },
  )
}

function podReadyCount(pod: ClusterWorkload): ReadyCount {
  if (pod.status === 'Succeeded') return { actual: 1, planned: 1 }
  if (pod.status === 'Failed') {
    const parsed = parseReady(pod.ready)
    return { actual: 0, planned: Math.max(parsed.planned, 1) }
  }
  return parseReady(pod.ready)
}

export function isDeploymentReady(deployment: ClusterWorkload): boolean {
  if (deployment.kind !== 'Deployment') return false
  const ready = parseReady(deployment.ready)
  if (ready.planned === 0) return deployment.reachability === 'ok'
  return deployment.reachability === 'ok' && ready.actual >= ready.planned
}

export function readyTone(count: ReadyCount, anyUnhealthy = false): ReadyTone {
  if (count.planned === 0) return 'idle'
  if (anyUnhealthy || (count.actual === 0 && count.planned > 0)) return 'fail'
  if (count.actual >= count.planned) return 'ok'
  return 'warn'
}

export function computeNamespaceReadyStats(workloads: ClusterWorkload[]): NamespaceReadyStats {
  const deployments = workloads.filter(w => w.kind === 'Deployment')
  const { orphanPods } = groupWorkloadsByDeployment(workloads)

  let deploymentOk = 0
  let deploymentFailed = 0
  for (const deployment of deployments) {
    if (isDeploymentReady(deployment)) deploymentOk += 1
    else deploymentFailed += 1
  }

  const deploymentReady = mergeReadyCounts(deployments.map(d => parseReady(d.ready)))
  const standaloneReady = mergeReadyCounts(orphanPods.map(podReadyCount))

  return {
    deploymentReady,
    standaloneReady,
    deploymentOk,
    deploymentFailed,
    deploymentTotal: deployments.length,
  }
}

export type CategoryDeployStats = { ok: number; failed: number; loading: boolean }

export function aggregateCategoryDeployStats(
  namespaceNames: string[],
  statsByNs: Map<string, NamespaceReadyStats | undefined>,
  loadingNames: Set<string>,
): CategoryDeployStats {
  let ok = 0
  let failed = 0
  let loading = false
  for (const name of namespaceNames) {
    if (loadingNames.has(name)) {
      loading = true
      continue
    }
    const stats = statsByNs.get(name)
    if (stats == null) continue
    ok += stats.deploymentOk
    failed += stats.deploymentFailed
  }
  return { ok, failed, loading }
}

export function namespaceNamesForFilter(filter: NsFilterType, allNamespaces: string[]): string[] {
  if (filter === 'all') return allNamespaces
  return allowedNamespaceNames(filter) ?? []
}

export function computeReadyTones(
  workloads: ClusterWorkload[],
  stats: NamespaceReadyStats,
): { deployment: ReadyTone; standalone: ReadyTone } {
  const { orphanPods } = groupWorkloadsByDeployment(workloads)
  const deploymentAnyFail = stats.deploymentFailed > 0
  const standaloneAnyFail = orphanPods.some(p => p.reachability === 'fail' || p.status === 'Failed')
  const standaloneRunningBehind = orphanPods.some(p => {
    if (p.status !== 'Running' && p.status !== 'Pending') return false
    const ready = parseReady(p.ready)
    return ready.planned > 0 && ready.actual < ready.planned
  })
  return {
    deployment: readyTone(stats.deploymentReady, deploymentAnyFail),
    standalone: readyTone(stats.standaloneReady, standaloneAnyFail || standaloneRunningBehind),
  }
}
