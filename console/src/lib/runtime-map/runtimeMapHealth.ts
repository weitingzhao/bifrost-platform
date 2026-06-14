import type { MatrixResponse, Target, TopologyResponse } from '@/api/types'

const REACH_ORDER: Record<string, number> = {
  fail: 0,
  degraded: 1,
  ok: 2,
  unknown: 3,
}

export function getFailingTargets(matrix: MatrixResponse | undefined): Target[] {
  if (!matrix) return []
  return matrix.targets
    .filter(t => t.reachability === 'fail')
    .sort((a, b) => a.id.localeCompare(b.id))
}

export function getPrimaryFailure(matrix: MatrixResponse | undefined): Target | undefined {
  return getFailingTargets(matrix)[0]
}

export function getAffectedNodeIds(
  topology: TopologyResponse | undefined,
  targetId: string,
): string[] {
  if (!topology) return []
  return topology.nodes
    .filter(n => n.matrix_services?.some(s => s.id === targetId))
    .map(n => n.id)
}

export function getAffectedNodeLabels(
  topology: TopologyResponse | undefined,
  targetId: string,
): string[] {
  if (!topology) return []
  return topology.nodes
    .filter(n => n.matrix_services?.some(s => s.id === targetId))
    .map(n => n.label)
}

export function sortTargetsByReachability(targets: Target[]): Target[] {
  return [...targets].sort(
    (a, b) =>
      (REACH_ORDER[a.reachability] ?? 9) - (REACH_ORDER[b.reachability] ?? 9) ||
      a.id.localeCompare(b.id),
  )
}

export function layerHasFailingTarget(
  layerTargetIds: string[],
  matrix: MatrixResponse | undefined,
): boolean {
  if (!matrix) return false
  const failIds = new Set(getFailingTargets(matrix).map(t => t.id))
  return layerTargetIds.some(id => failIds.has(id))
}
