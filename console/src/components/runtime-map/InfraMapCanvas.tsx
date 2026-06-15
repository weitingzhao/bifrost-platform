import { useMemo, useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import type { TopologyResponse } from '@/api/types'
import { buildGapOverview, type GapOverview } from '@/lib/runtime-map/gapAnalysis'
import {
  buildInfraMapLayout,
  edgeDash,
  edgeMidpoint,
  edgeStrokeClass,
  isSelfLoopEdge,
  localEdgesForNode,
  pipePath,
  type LayoutMode,
} from '@/lib/runtime-map/infraMapLayout'
import { type RoleView, type StackChipModel } from '@/lib/runtime-map/roleComponentRegistry'
import { HostBay } from './HostBay'

interface InfraMapCanvasProps {
  data: TopologyResponse
  clusterLive?: boolean
  onOpenCluster?: () => void
  roleView: RoleView
  onRoleViewChange: (view: RoleView) => void
  layoutMode: LayoutMode
  onLayoutModeChange: (mode: LayoutMode) => void
  showGhostOverlay?: boolean
  onToggleGhostOverlay?: () => void
  selectedNodeId: string | null
  selectedEdgeId: string | null
  highlightNodeIds: string[]
  highlightEdgeIds?: string[]
  highlightChipIds?: string[]
  selectedChipId?: string | null
  onSelectNode: (nodeId: string) => void
  onSelectEdge?: (edgeId: string) => void
  onSelectChip?: (chip: StackChipModel, nodeId: string) => void
}

export function InfraMapCanvas({
  data,
  clusterLive,
  onOpenCluster,
  roleView,
  onRoleViewChange,
  layoutMode,
  onLayoutModeChange,
  showGhostOverlay,
  onToggleGhostOverlay,
  selectedNodeId,
  selectedEdgeId,
  highlightNodeIds,
  highlightEdgeIds = [],
  highlightChipIds = [],
  selectedChipId,
  onSelectNode,
  onSelectEdge,
  onSelectChip,
}: InfraMapCanvasProps) {
  const [hoveredEdgeId, setHoveredEdgeId] = useState<string | null>(null)

  const { viewBox, nodeLayout } = useMemo(
    () => buildInfraMapLayout(data, { mode: layoutMode }),
    [data, layoutMode],
  )

  const gapOverview: GapOverview = useMemo(
    () => buildGapOverview(data, undefined, roleView),
    [data, roleView],
  )
  const gapByNode = useMemo(() => {
    const map = new Map<string, (typeof gapOverview.nodeGaps)[0]>()
    for (const ng of gapOverview.nodeGaps) map.set(ng.nodeId, ng)
    return map
  }, [gapOverview])
  const highlightNodeSet = useMemo(() => new Set(highlightNodeIds), [highlightNodeIds])
  const highlightEdgeSet = useMemo(() => new Set(highlightEdgeIds), [highlightEdgeIds])
  const highlightChipSet = useMemo(() => new Set(highlightChipIds), [highlightChipIds])

  const drawableEdges = useMemo(
    () => data.edges.filter(e => !isSelfLoopEdge(e)),
    [data.edges],
  )

  return (
    <div className="infra-map-shell">
      <header className="infra-map-header">
        <div className="flex flex-wrap items-center gap-2 min-w-0">
          <h2 className="m-0 text-sm font-semibold">Hardware topology</h2>
          <span className="badge-ui">{data.label}</span>
          <span className="badge-ui">phase: {data.deployment_phase}</span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            className="btn-ui btn-ui-ghost text-xs"
            onClick={() => onLayoutModeChange(layoutMode === 'dataPath' ? 'grid' : 'dataPath')}
          >
            {layoutMode === 'dataPath' ? 'Grid layout' : 'Data-path layout'}
          </button>
          {onToggleGhostOverlay != null && roleView === 'k3s' && data.deployment_phase === 'compose' && (
            <button
              type="button"
              className={['btn-ui btn-ui-ghost text-xs', showGhostOverlay ? 'btn-ui-active' : '']
                .filter(Boolean)
                .join(' ')}
              onClick={onToggleGhostOverlay}
            >
              {showGhostOverlay ? 'Hide planned overlay' : 'Show planned overlay'}
            </button>
          )}
          <SegmentControl
            ariaLabel="Role view"
            value={roleView}
            onChange={v => onRoleViewChange(v as RoleView)}
            options={[
              { value: 'compose', label: 'Compose' },
              { value: 'k3s', label: 'K3s target' },
            ]}
          />
        </div>
      </header>

      <footer className="infra-map-legend infra-map-legend--top">
        <span className="infra-map-legend__item">
          <span className="infra-map-legend__swatch infra-edge--ok" /> Host OK
        </span>
        <span className="infra-map-legend__item">
          <span className="infra-map-legend__swatch infra-edge--kind-data" /> Data
        </span>
        <span className="infra-map-legend__item">
          <span className="infra-map-legend__swatch infra-edge--kind-http" /> HTTP
        </span>
        <span className="infra-map-legend__item">
          <span className="infra-map-legend__swatch infra-edge--kind-ib" /> IB
        </span>
        <span className="infra-map-legend__item">
          <span className="infra-stack-chip infra-stack-chip--planned infra-map-legend__chip-demo">
            planned
          </span>
        </span>
      </footer>

      <div
        className="infra-map-canvas"
        style={{ aspectRatio: `${viewBox.width} / ${viewBox.height}` }}
      >
        <svg
          className="infra-map-svg"
          viewBox={`0 0 ${viewBox.width} ${viewBox.height}`}
          preserveAspectRatio="xMidYMid meet"
          aria-hidden
        >
          <defs>
            <pattern id="infra-map-grid" width="20" height="20" patternUnits="userSpaceOnUse">
              <path
                d="M 20 0 L 0 0 0 20"
                fill="none"
                stroke="var(--border)"
                strokeOpacity="0.35"
                strokeWidth="0.5"
              />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#infra-map-grid)" className="infra-map-grid-bg" />

          {drawableEdges.map(edge => {
            const fromRect = nodeLayout[edge.from]
            const toRect = nodeLayout[edge.to]
            if (!fromRect || !toRect) return null
            const path = pipePath(fromRect, toRect, false)
            const mid = edgeMidpoint(path, fromRect, toRect, false)
            const selected = selectedEdgeId === edge.id
            const highlighted = highlightEdgeSet.has(edge.id)
            const hovered = hoveredEdgeId === edge.id
            const showLabel = selected || highlighted || hovered

            return (
              <g key={edge.id} className="infra-edge-group">
                <path
                  d={path}
                  className={[
                    'infra-edge',
                    edgeStrokeClass(edge.status),
                    selected ? 'infra-edge--selected' : '',
                    highlighted ? 'infra-edge--highlight' : '',
                    hovered ? 'infra-edge--hover' : '',
                    `infra-edge--kind-${edge.kind}`,
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  fill="none"
                  strokeWidth={selected || highlighted || hovered ? 2.5 : 1.75}
                  strokeDasharray={edgeDash(edge.kind, false)}
                  onClick={() => onSelectEdge?.(edge.id)}
                  onMouseEnter={() => setHoveredEdgeId(edge.id)}
                  onMouseLeave={() => setHoveredEdgeId(null)}
                  style={{ pointerEvents: 'stroke', cursor: onSelectEdge ? 'pointer' : 'default' }}
                />
                {showLabel && (
                  <text
                    x={mid.x}
                    y={mid.y}
                    className="infra-edge-label"
                    textAnchor="middle"
                    dominantBaseline="middle"
                  >
                    {edge.label}
                  </text>
                )}
              </g>
            )
          })}
        </svg>

        <div className="infra-map-nodes">
          {data.nodes.map(node => {
            const rect = nodeLayout[node.id]
            if (!rect) return null
            return (
              <HostBay
                key={node.id}
                node={node}
                rect={rect}
                viewBox={viewBox}
                roleView={roleView}
                clusterLive={clusterLive}
                onOpenCluster={onOpenCluster}
                showGhostOverlay={showGhostOverlay}
                selected={selectedNodeId === node.id}
                highlighted={highlightNodeSet.has(node.id)}
                expanded={highlightNodeSet.has(node.id)}
                localEdges={localEdgesForNode(node.id, data.edges)}
                highlightedChipIds={highlightChipSet}
                selectedChipId={selectedChipId}
                gap={gapByNode.get(node.id)}
                onSelectNode={() => onSelectNode(node.id)}
                onSelectChip={
                  onSelectChip ? chip => onSelectChip(chip, node.id) : undefined
                }
              />
            )
          })}
        </div>
      </div>
    </div>
  )
}
