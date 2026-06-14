import type { MatrixResponse, Target, TopologyNode, TopologyResponse } from '@/api/types'
import { HARDWARE_ROWS, SCOPE_ROWS, type ScopeRow } from '@/lib/environments-catalog'

export type ScopeTag = ScopeRow['tag']

export type ScopeLayer = {
  tag: ScopeTag
  component: string
  technology: string
  notes: string
  /** Matrix target ids associated with this scope layer */
  targetIds: string[]
  /** True when no matrix probes apply (planning-only) */
  plannedOnly: boolean
}

const PLANNED_ONLY_TAGS = new Set<ScopeTag>(['K3S', 'OBSERVE', 'AI', 'GITHUB'])

const TARGET_SCOPE_RULES: Array<{ match: (id: string) => boolean; tag: ScopeTag }> = [
  { match: id => id === 'api-ops' || id === 'ops-capabilities', tag: 'PLATFORM' },
  { match: id => id.startsWith('api-'), tag: 'TRADE-API' },
  { match: id => id === 'nginx-spa', tag: 'INFRA' },
  { match: id => id === 'postgres', tag: 'PG' },
  { match: id => id === 'redis', tag: 'REDIS' },
]

export function matrixTargetToScopeTag(targetId: string): ScopeTag | null {
  for (const rule of TARGET_SCOPE_RULES) {
    if (rule.match(targetId)) return rule.tag
  }
  return null
}

export function buildScopeLayers(matrix: MatrixResponse | undefined): ScopeLayer[] {
  const targets = matrix?.targets ?? []
  const targetsByTag = new Map<ScopeTag, string[]>()

  for (const t of targets) {
    const tag = matrixTargetToScopeTag(t.id)
    if (tag == null) continue
    const list = targetsByTag.get(tag) ?? []
    list.push(t.id)
    targetsByTag.set(tag, list)
  }

  return SCOPE_ROWS.map(row => {
    const fromMatrix = targetsByTag.get(row.tag) ?? []
    const plannedOnly = PLANNED_ONLY_TAGS.has(row.tag) && fromMatrix.length === 0
    return {
      tag: row.tag,
      component: row.component,
      technology: row.technology,
      notes: row.notes,
      targetIds: fromMatrix,
      plannedOnly,
    }
  })
}

export function nodeToHardwareMeta(nodeId: string) {
  return HARDWARE_ROWS.find(h => h.id === nodeId)
}

export function hostRoleSummary(nodeId: string, roleView: 'compose' | 'k3s'): string | undefined {
  const hw = nodeToHardwareMeta(nodeId)
  if (!hw) return undefined
  return roleView === 'compose' ? hw.roleCompose : hw.roleK3s
}

export function filterTargetsForNode(
  node: TopologyNode | undefined,
  matrix: MatrixResponse | undefined,
): Target[] {
  if (!node || !matrix) return []
  const ids = new Set(node.matrix_services?.map(s => s.id) ?? [])
  return matrix.targets.filter(t => ids.has(t.id))
}

export function filterScopeLayersForNode(
  node: TopologyNode | undefined,
  layers: ScopeLayer[],
): ScopeLayer[] {
  if (!node) return layers
  const serviceIds = new Set(node.matrix_services?.map(s => s.id) ?? [])
  if (serviceIds.size === 0) return []

  return layers
    .map(layer => ({
      ...layer,
      targetIds: layer.targetIds.filter(id => serviceIds.has(id)),
    }))
    .filter(layer => layer.targetIds.length > 0 || layer.plannedOnly)
}

export function filterNodesByTarget(
  targetId: string,
  topology: TopologyResponse,
): string[] {
  return topology.nodes
    .filter(n => n.matrix_services?.some(s => s.id === targetId))
    .map(n => n.id)
}

export function countServiceStats(services: TopologyNode['matrix_services'] | undefined): {
  total: number
  fail: number
  ok: number
} {
  const list = services ?? []
  let fail = 0
  let ok = 0
  for (const s of list) {
    if (s.reachability === 'fail') fail += 1
    else if (s.reachability === 'ok' || s.reachability === 'degraded') ok += 1
  }
  return { total: list.length, fail, ok }
}

export type RuntimeMapSelection =
  | { kind: 'node'; id: string }
  | { kind: 'target'; id: string }
  | { kind: 'scope'; tag: ScopeTag }
  | { kind: 'edge'; id: string }
  | null

export function getEdge(topology: TopologyResponse, edgeId: string) {
  return topology.edges.find(e => e.id === edgeId)
}

export function filterEdgesByTarget(
  targetId: string,
  topology: TopologyResponse,
): string[] {
  return topology.edges.filter(e => e.matrix_target === targetId).map(e => e.id)
}

export function getNode(topology: TopologyResponse, nodeId: string): TopologyNode | undefined {
  return topology.nodes.find(n => n.id === nodeId)
}

export function getTarget(matrix: MatrixResponse | undefined, targetId: string): Target | undefined {
  return matrix?.targets.find(t => t.id === targetId)
}

export function getScopeLayer(layers: ScopeLayer[], tag: ScopeTag): ScopeLayer | undefined {
  return layers.find(l => l.tag === tag)
}
