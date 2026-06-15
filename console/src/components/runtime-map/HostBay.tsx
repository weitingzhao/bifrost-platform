import { useMemo } from 'react'
import type { CSSProperties } from 'react'
import type { TopologyEdge, TopologyNode } from '@/api/types'
import { ComponentIcon } from '@/components/runtime-map/ComponentIcon'
import { GapProgressBar } from '@/components/runtime-map/GapProgressBar'
import { StatusLamp } from '@/components/StatusLamp'
import type { NodeGapSummary } from '@/lib/runtime-map/gapAnalysis'
import type { NodeRect } from '@/lib/runtime-map/infraMapLayout'
import { nodeRectStyle } from '@/lib/runtime-map/infraMapLayout'
import type { InfraMapViewBox } from '@/lib/runtime-map/infraMapLayout'
import { chipsForNode, type RoleView, type StackChipModel } from '@/lib/runtime-map/roleComponentRegistry'
import { countServiceStats, hostRoleSummary } from '@/lib/runtime-map/runtimeMapRegistry'
import { StackChip } from './StackChip'

function pctClass(pct: number): string {
  if (pct >= 80) return 'gap-pct--high'
  if (pct >= 40) return 'gap-pct--mid'
  if (pct > 0) return 'gap-pct--low'
  return 'gap-pct--zero'
}

const GROUP_LABELS: Record<string, string> = {
  external: 'External',
  linux: 'Linux',
  mac: 'Mac',
  compute: 'GPU',
}

const TILE_LIMIT = 4

const REACH_ORDER: Record<string, number> = {
  fail: 0,
  degraded: 1,
  ok: 2,
  unknown: 3,
}

interface HostBayProps {
  node: TopologyNode
  rect: NodeRect
  viewBox: InfraMapViewBox
  roleView: RoleView
  clusterLive?: boolean
  onOpenCluster?: () => void
  showGhostOverlay?: boolean
  selected?: boolean
  highlighted?: boolean
  expanded?: boolean
  localEdges?: TopologyEdge[]
  highlightedChipIds?: Set<string>
  selectedChipId?: string | null
  gap?: NodeGapSummary
  onSelectNode: () => void
  onSelectChip?: (chip: StackChipModel) => void
}

export function HostBay({
  node,
  rect,
  viewBox,
  roleView,
  clusterLive,
  onOpenCluster,
  showGhostOverlay,
  selected,
  highlighted,
  expanded,
  localEdges = [],
  highlightedChipIds,
  selectedChipId,
  gap,
  onSelectNode,
  onSelectChip,
}: HostBayProps) {
  const stats = countServiceStats(node.matrix_services ?? [])
  const chips = useMemo(
    () =>
      [...chipsForNode(node, roleView, { showGhostOverlay })].sort(
        (a, b) =>
          (REACH_ORDER[a.reachability] ?? 9) - (REACH_ORDER[b.reachability] ?? 9) ||
          a.label.localeCompare(b.label),
      ),
    [node, roleView, showGhostOverlay],
  )

  const showExpanded = expanded || selected
  const visibleTiles = chips.filter(c => !c.ghost).slice(0, TILE_LIMIT)
  const overflowCount = Math.max(0, chips.filter(c => !c.ghost).length - TILE_LIMIT)
  const selfLoops = localEdges.length > 0 ? localEdges : []
  const roleSummary = hostRoleSummary(node.id, roleView)

  const showLiveBadge =
    clusterLive === true &&
    roleView === 'k3s' &&
    (node.id === 'mini-pc-c' || node.host === '192.168.10.73')

  return (
    <div
      className={[
        'infra-host-bay',
        selected ? 'infra-host-bay--selected' : '',
        highlighted ? 'infra-host-bay--highlight' : '',
        showExpanded ? 'infra-host-bay--expanded' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      style={nodeRectStyle(rect, viewBox)}
    >
      <button
        type="button"
        className="infra-host-bay__header"
        onClick={onSelectNode}
        title={`${node.label} · ${node.status}`}
      >
        <div className="infra-host-bay__title-row">
          <StatusLamp value={node.status} kind="reach" />
          <strong className="infra-host-bay__title">{node.label}</strong>
          {showLiveBadge && onOpenCluster != null && (
            <button
              type="button"
              className="badge-ui badge-status-deployed text-[10px] ml-1"
              onClick={e => {
                e.stopPropagation()
                onOpenCluster()
              }}
            >
              Live
            </button>
          )}
          {showLiveBadge && onOpenCluster == null && (
            <span className="badge-ui badge-status-deployed text-[10px] ml-1">Live</span>
          )}
        </div>
        {roleSummary != null && (
          <div className="infra-host-bay__role" title={roleSummary}>
            {roleSummary}
          </div>
        )}
        <div className="infra-host-bay__meta">
          {GROUP_LABELS[node.group] ?? node.group}
          {node.host ? ` · ${node.host}` : ''}
        </div>
        {stats.total > 0 && (
          <div className="infra-host-bay__stats">
            <span className="badge-ui font-mono-tabular text-[10px]">
              {stats.ok}/{stats.total}
            </span>
            {stats.fail > 0 && (
              <span className="badge-ui badge-status-blocked font-mono-tabular text-[10px]">
                {stats.fail} fail
              </span>
            )}
          </div>
        )}
      </button>

      {gap != null && gap.totalCount > 0 && (
        <div className="infra-host-bay__gap-bar">
          <GapProgressBar gap={gap} />
        </div>
      )}
      {gap != null && gap.totalCount > 0 && (
        <div className="infra-host-bay__gap-label">
          <span>{gap.liveCount}/{gap.totalCount} live</span>
          <span className={`gap-pct ${pctClass(gap.completionPct)}`}>
            {gap.completionPct}%
          </span>
        </div>
      )}

      {selfLoops.length > 0 && (
        <div className="infra-host-bay__local-edges">
          {selfLoops.map(edge => (
            <span key={edge.id} className="infra-host-bay__local-badge" title={edge.detail}>
              Local: {edge.label}
            </span>
          ))}
        </div>
      )}

      <div className="infra-host-bay__chips" data-role-view={roleView}>
        {showExpanded ? (
          chips.map(chip => (
            <StackChip
              key={`${roleView}-${chip.chipId}`}
              chip={chip}
              roleView={roleView}
              selected={selectedChipId === chip.chipId}
              highlighted={highlightedChipIds?.has(chip.chipId)}
              onClick={onSelectChip ? () => onSelectChip(chip) : undefined}
            />
          ))
        ) : (
          <div className="infra-host-bay__tile-row">
            {visibleTiles.map(chip => (
              <button
                key={`tile-${roleView}-${chip.chipId}`}
                type="button"
                className={[
                  'infra-host-bay__tile',
                  selectedChipId === chip.chipId ? 'infra-host-bay__tile--selected' : '',
                  highlightedChipIds?.has(chip.chipId) ? 'infra-host-bay__tile--highlight' : '',
                  chip.reachability === 'fail' ? 'infra-host-bay__tile--fail' : '',
                  chip.planned ? 'infra-host-bay__tile--planned' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                style={{ '--chip-brand': chip.brandColor } as CSSProperties}
                title={chip.label}
                onClick={onSelectChip ? () => onSelectChip(chip) : undefined}
              >
                <ComponentIcon
                  componentId={chip.componentId}
                  variant="tile"
                  showWell={chip.componentId !== 'ib'}
                />
              </button>
            ))}
            {overflowCount > 0 && (
              <span className="infra-host-bay__tile-overflow">+{overflowCount}</span>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
