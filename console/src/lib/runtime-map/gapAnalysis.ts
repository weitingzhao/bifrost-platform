/**
 * Gap Analysis — planned vs live state comparison for Runtime Map.
 *
 * "Live" = service/component has a matrix probe returning ok/degraded/fail.
 * "Planned" = defined in topology roles but no live probe (reachability === 'unknown').
 */

import type { MatrixResponse, TopologyNode, TopologyResponse } from '@/api/types'
import {
  chipsForNode,
  type RoleView,
  type StackChipModel,
} from '@/lib/runtime-map/infraVisualRegistry'
import { HARDWARE_ROWS } from '@/lib/environments-catalog'
import { buildScopeLayers, type ScopeLayer } from '@/lib/runtime-map/runtimeMapRegistry'

export type ServiceGapStatus = 'live' | 'planned' | 'degraded' | 'fail'

export type ServiceGapItem = {
  chipId: string
  componentId: string
  label: string
  status: ServiceGapStatus
  brandColor: string
  scopeTag?: string
}

export type NodeGapSummary = {
  nodeId: string
  nodeLabel: string
  host: string
  group: string
  inCluster: boolean
  liveCount: number
  plannedCount: number
  failCount: number
  degradedCount: number
  totalCount: number
  completionPct: number
  services: ServiceGapItem[]
  roleCompose: string
  roleK3s: string
}

export type ScopeGapSummary = {
  tag: string
  component: string
  technology: string
  liveTargets: number
  totalTargets: number
  plannedOnly: boolean
  status: 'live' | 'partial' | 'planned'
}

export type GapOverview = {
  totalComponents: number
  liveComponents: number
  plannedComponents: number
  failComponents: number
  degradedComponents: number
  overallCompletionPct: number
  nodeGaps: NodeGapSummary[]
  scopeGaps: ScopeGapSummary[]
  topGapNodes: NodeGapSummary[]
}

function chipToGapStatus(chip: StackChipModel): ServiceGapStatus {
  if (chip.planned || chip.reachability === 'unknown') return 'planned'
  if (chip.reachability === 'fail') return 'fail'
  if (chip.reachability === 'degraded') return 'degraded'
  return 'live'
}

function buildNodeGap(
  node: TopologyNode,
  roleView: RoleView,
): NodeGapSummary {
  const chips = chipsForNode(node, roleView)
  const hw = HARDWARE_ROWS.find(h => h.id === node.id)

  const services: ServiceGapItem[] = chips
    .filter(c => !c.ghost)
    .map(c => ({
      chipId: c.chipId,
      componentId: c.componentId,
      label: c.label,
      status: chipToGapStatus(c),
      brandColor: c.brandColor,
      scopeTag: c.scopeTag,
    }))

  const liveCount = services.filter(s => s.status === 'live').length
  const plannedCount = services.filter(s => s.status === 'planned').length
  const failCount = services.filter(s => s.status === 'fail').length
  const degradedCount = services.filter(s => s.status === 'degraded').length
  const totalCount = services.length

  return {
    nodeId: node.id,
    nodeLabel: node.label,
    host: node.host ?? '',
    group: node.group ?? 'unknown',
    inCluster: node.in_k3s_cluster ?? false,
    liveCount,
    plannedCount,
    failCount,
    degradedCount,
    totalCount,
    completionPct: totalCount > 0 ? Math.round((liveCount / totalCount) * 100) : 0,
    services,
    roleCompose: hw?.roleCompose ?? '',
    roleK3s: hw?.roleK3s ?? '',
  }
}

function buildScopeGap(layer: ScopeLayer, matrix: MatrixResponse | undefined): ScopeGapSummary {
  const targets = matrix?.targets.filter(t => layer.targetIds.includes(t.id)) ?? []
  const liveTargets = targets.filter(
    t => t.reachability === 'ok' || t.reachability === 'degraded',
  ).length

  let status: ScopeGapSummary['status'] = 'planned'
  if (layer.plannedOnly || targets.length === 0) {
    status = 'planned'
  } else if (liveTargets === targets.length) {
    status = 'live'
  } else {
    status = 'partial'
  }

  return {
    tag: layer.tag,
    component: layer.component,
    technology: layer.technology,
    liveTargets,
    totalTargets: targets.length,
    plannedOnly: layer.plannedOnly,
    status,
  }
}

export function buildGapOverview(
  topology: TopologyResponse | undefined,
  matrix: MatrixResponse | undefined,
  roleView: RoleView,
): GapOverview {
  if (!topology) {
    return {
      totalComponents: 0,
      liveComponents: 0,
      plannedComponents: 0,
      failComponents: 0,
      degradedComponents: 0,
      overallCompletionPct: 0,
      nodeGaps: [],
      scopeGaps: [],
      topGapNodes: [],
    }
  }

  const nodeGaps = topology.nodes.map(n => buildNodeGap(n, roleView))

  const totalComponents = nodeGaps.reduce((s, n) => s + n.totalCount, 0)
  const liveComponents = nodeGaps.reduce((s, n) => s + n.liveCount, 0)
  const plannedComponents = nodeGaps.reduce((s, n) => s + n.plannedCount, 0)
  const failComponents = nodeGaps.reduce((s, n) => s + n.failCount, 0)
  const degradedComponents = nodeGaps.reduce((s, n) => s + n.degradedCount, 0)

  const scopeLayers = buildScopeLayers(matrix)
  const scopeGaps = scopeLayers.map(l => buildScopeGap(l, matrix))

  const topGapNodes = [...nodeGaps]
    .filter(n => n.plannedCount > 0 || n.failCount > 0)
    .sort((a, b) => b.plannedCount - a.plannedCount || b.failCount - a.failCount)

  return {
    totalComponents,
    liveComponents,
    plannedComponents,
    failComponents,
    degradedComponents,
    overallCompletionPct:
      totalComponents > 0 ? Math.round((liveComponents / totalComponents) * 100) : 0,
    nodeGaps,
    scopeGaps,
    topGapNodes,
  }
}

/** Format gap overview as text for LLM consumption. */
export function formatGapForLlm(gap: GapOverview, roleView: RoleView): string {
  const lines: string[] = [
    `## Gap Analysis (${roleView} view)`,
    '',
    `Overall completion: ${gap.overallCompletionPct}% (${gap.liveComponents} live / ${gap.totalComponents} total)`,
    `- Live: ${gap.liveComponents}`,
    `- Planned (not yet live): ${gap.plannedComponents}`,
    `- Failing: ${gap.failComponents}`,
    `- Degraded: ${gap.degradedComponents}`,
    '',
    '### Per-server gap',
  ]

  for (const node of gap.nodeGaps) {
    const bar = `${node.liveCount}/${node.totalCount}`
    const pct = `${node.completionPct}%`
    lines.push(`- **${node.nodeLabel}** (${node.host || 'no IP'}): ${bar} live (${pct})`)
    if (node.plannedCount > 0) {
      const planned = node.services
        .filter(s => s.status === 'planned')
        .map(s => s.label)
      lines.push(`  - Planned: ${planned.join(', ')}`)
    }
    if (node.failCount > 0) {
      const failing = node.services
        .filter(s => s.status === 'fail')
        .map(s => s.label)
      lines.push(`  - Failing: ${failing.join(', ')}`)
    }
  }

  if (gap.topGapNodes.length > 0) {
    lines.push('')
    lines.push('### Priority gap nodes (most planned components)')
    for (const node of gap.topGapNodes.slice(0, 5)) {
      lines.push(
        `- ${node.nodeLabel}: ${node.plannedCount} planned, ${node.failCount} failing, completion ${node.completionPct}%`,
      )
    }
  }

  lines.push('')
  lines.push('### Scope layer status')
  for (const scope of gap.scopeGaps) {
    const statusLabel =
      scope.status === 'live'
        ? 'LIVE'
        : scope.status === 'partial'
          ? 'PARTIAL'
          : 'PLANNED'
    lines.push(`- **${scope.tag}** ${scope.component}: ${statusLabel}`)
    if (scope.status !== 'planned') {
      lines.push(`  - Probes: ${scope.liveTargets}/${scope.totalTargets} live`)
    }
  }

  return lines.join('\n')
}
