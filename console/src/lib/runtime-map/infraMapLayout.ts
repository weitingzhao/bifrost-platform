import type { CSSProperties } from 'react'
import type { TopologyEdge, TopologyNode, TopologyResponse } from '@/api/types'

export const INFRA_MAP_CELL_W = 200
export const INFRA_MAP_CELL_H = 172
export const INFRA_MAP_PAD_X = 24
export const INFRA_MAP_PAD_Y = 24

export type LayoutMode = 'grid' | 'dataPath'

export type NodeRect = {
  x: number
  y: number
  w: number
  h: number
  cx: number
  cy: number
}

export type InfraMapViewBox = {
  width: number
  height: number
}

/** Semantic grid overrides for data-path readability (yaml grid remains source of truth for fallback). */
const SEMANTIC_GRID_OVERRIDES: Record<string, { row: number; col: number }> = {
  'mini-pc-a': { row: 1, col: 1 },
  'mini-pc-b': { row: 1, col: 2 },
  'win11-host': { row: 0, col: 1 },
  'win11-secondary': { row: 0, col: 2 },
  'mac-mini-1': { row: 2, col: 0 },
  macbook: { row: 2, col: 3 },
  'mini-pc-c': { row: 1, col: 3 },
  'gpu-server': { row: 2, col: 1 },
  'mac-mini-2': { row: 2, col: 2 },
}

function effectiveGrid(node: TopologyNode, mode: LayoutMode): { row: number; col: number } {
  if (mode === 'dataPath' && SEMANTIC_GRID_OVERRIDES[node.id]) {
    return SEMANTIC_GRID_OVERRIDES[node.id]
  }
  return node.grid
}

export function buildInfraMapLayout(
  topology: TopologyResponse,
  options?: { mode?: LayoutMode },
): {
  viewBox: InfraMapViewBox
  nodeLayout: Record<string, NodeRect>
  mode: LayoutMode
} {
  const mode = options?.mode ?? 'dataPath'
  let maxRow = 0
  let maxCol = 0

  for (const n of topology.nodes) {
    const g = effectiveGrid(n, mode)
    if (g.row > maxRow) maxRow = g.row
    if (g.col > maxCol) maxCol = g.col
  }

  const width = INFRA_MAP_PAD_X * 2 + (maxCol + 1) * INFRA_MAP_CELL_W
  const height = INFRA_MAP_PAD_Y * 2 + (maxRow + 1) * INFRA_MAP_CELL_H

  const nodeLayout: Record<string, NodeRect> = {}
  for (const n of topology.nodes) {
    const g = effectiveGrid(n, mode)
    const x = INFRA_MAP_PAD_X + g.col * INFRA_MAP_CELL_W
    const y = INFRA_MAP_PAD_Y + g.row * INFRA_MAP_CELL_H
    const w = INFRA_MAP_CELL_W - 16
    const h = INFRA_MAP_CELL_H - 16
    nodeLayout[n.id] = {
      x,
      y,
      w,
      h,
      cx: x + w / 2,
      cy: y + h / 2,
    }
  }

  return { viewBox: { width, height }, nodeLayout, mode }
}

export function nodeAnchorPoint(
  from: NodeRect,
  to: NodeRect,
): { x: number; y: number } {
  const dx = to.cx - from.cx
  const dy = to.cy - from.cy
  if (Math.abs(dx) >= Math.abs(dy)) {
    return { x: from.x + (dx > 0 ? from.w : 0), y: from.cy }
  }
  return { x: from.cx, y: from.y + (dy > 0 ? from.h : 0) }
}

export function pipePath(
  from: NodeRect,
  to: NodeRect,
  sameNode: boolean,
): string {
  if (sameNode) {
    const loopR = 28
    const ax = from.x + from.w
    const ay = from.cy - loopR
    return `M ${ax} ${from.cy} C ${ax + loopR} ${from.cy}, ${ax + loopR} ${ay}, ${ax} ${ay} C ${ax - loopR} ${ay}, ${ax - loopR} ${from.cy + loopR}, ${from.x + from.w * 0.5} ${from.cy + loopR}`
  }
  return pipePathOrthogonal(from, to)
}

export function pipePathOrthogonal(from: NodeRect, to: NodeRect): string {
  const a = nodeAnchorPoint(from, to)
  const b = nodeAnchorPoint(to, from)
  const midX = (a.x + b.x) / 2
  return `M ${a.x} ${a.y} L ${midX} ${a.y} L ${midX} ${b.y} L ${b.x} ${b.y}`
}

export function edgeMidpoint(_path: string, from: NodeRect, to: NodeRect, sameNode: boolean): {
  x: number
  y: number
} {
  if (sameNode) {
    return { x: from.x + from.w + 20, y: from.cy - 20 }
  }
  const a = nodeAnchorPoint(from, to)
  const b = nodeAnchorPoint(to, from)
  const midX = (a.x + b.x) / 2
  return { x: midX, y: (a.y + b.y) / 2 }
}

export function edgeStrokeClass(status: TopologyEdge['status']): string {
  if (status === 'ok') return 'infra-edge--ok'
  if (status === 'degraded') return 'infra-edge--degraded'
  if (status === 'fail') return 'infra-edge--fail'
  return 'infra-edge--unknown'
}

export function edgeDash(kind: string, sameNode: boolean): string | undefined {
  if (sameNode) return '6 4'
  if (kind === 'http' || kind === 'control') return '8 5'
  if (kind === 'ib') return '2 4'
  return undefined
}

export function isSelfLoopEdge(edge: TopologyEdge): boolean {
  return edge.from === edge.to
}

export function localEdgesForNode(
  nodeId: string,
  edges: TopologyEdge[],
): TopologyEdge[] {
  return edges.filter(e => e.from === nodeId && e.to === nodeId)
}

export function pct(value: number, total: number): string {
  return `${(value / total) * 100}%`
}

export function nodeRectStyle(rect: NodeRect, viewBox: InfraMapViewBox): CSSProperties {
  return {
    left: pct(rect.x, viewBox.width),
    top: pct(rect.y, viewBox.height),
    width: pct(rect.w, viewBox.width),
    height: pct(rect.h, viewBox.height),
  }
}
