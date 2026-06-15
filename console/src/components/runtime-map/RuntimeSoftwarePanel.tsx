import { useEffect, useMemo, useRef, useState } from 'react'
import type { MatrixResponse, OpsContextResponse, TopologyResponse } from '@/api/types'
import { ComponentIcon } from '@/components/runtime-map/ComponentIcon'
import { StatusLamp } from '@/components/StatusLamp'
import { summarizeMatrix } from '@/lib/control-room/matrixSummary'
import type { GapOverview, ScopeGapSummary } from '@/lib/runtime-map/gapAnalysis'
import { componentIdForScopeTag } from '@/lib/runtime-map/infraVisualRegistry'
import {
  getFailingTargets,
  layerHasFailingTarget,
  sortTargetsByReachability,
} from '@/lib/runtime-map/runtimeMapHealth'
import {
  buildScopeLayers,
  filterScopeLayersForNode,
  getNode,
  matrixTargetToScopeTag,
  type RuntimeMapSelection,
  type ScopeLayer,
  type ScopeTag,
} from '@/lib/runtime-map/runtimeMapRegistry'

interface RuntimeSoftwarePanelProps {
  matrix: MatrixResponse | undefined
  context: OpsContextResponse | undefined
  topology: TopologyResponse | undefined
  selection: RuntimeMapSelection
  gapOverview?: GapOverview
  onSelectTarget: (targetId: string) => void
  onSelectScope: (tag: ScopeTag) => void
}

export function RuntimeSoftwarePanel({
  matrix,
  context,
  topology,
  selection,
  gapOverview,
  onSelectTarget,
  onSelectScope,
}: RuntimeSoftwarePanelProps) {
  const allLayers = useMemo(() => buildScopeLayers(matrix), [matrix])
  const failCount = getFailingTargets(matrix).length
  const [showAllLayers, setShowAllLayers] = useState(false)

  const selectedNode =
    selection?.kind === 'node' && topology != null ? getNode(topology, selection.id) : undefined

  const baseLayers = useMemo(() => {
    if (selectedNode) return filterScopeLayersForNode(selectedNode, allLayers)
    return allLayers
  }, [allLayers, selectedNode])

  const layers = useMemo(() => {
    if (showAllLayers || failCount === 0) return baseLayers
    return baseLayers.filter(layer => layerHasFailingTarget(layer.targetIds, matrix))
  }, [baseLayers, showAllLayers, failCount, matrix])

  const summary = matrix != null ? summarizeMatrix(matrix) : null
  const listRef = useRef<HTMLDivElement>(null)

  const scopeGapMap = useMemo(() => {
    const map = new Map<string, ScopeGapSummary>()
    for (const sg of gapOverview?.scopeGaps ?? []) map.set(sg.tag, sg)
    return map
  }, [gapOverview])

  useEffect(() => {
    if (selection?.kind !== 'target' && selection?.kind !== 'scope') return
    const root = listRef.current
    if (!root) return
    const selector =
      selection.kind === 'target'
        ? `[data-target-id="${selection.id}"]`
        : `[data-scope-tag="${selection.tag}"]`
    const el = root.querySelector(selector)
    el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  }, [selection])

  return (
    <section className="page-section panel-elevated p-3 flex flex-col gap-3 runtime-software-panel min-h-0">
      <header>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="m-0 text-sm font-semibold">Software stack & probes</h2>
          {failCount > 0 && (
            <button
              type="button"
              className="btn-ui btn-ui-ghost text-xs"
              onClick={() => setShowAllLayers(v => !v)}
            >
              {showAllLayers
                ? 'Show failures only'
                : `Show all layers (${baseLayers.length})`}
            </button>
          )}
        </div>
        {summary != null && matrix != null && (
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {matrix.label}: ok {summary.ok} · fail {summary.fail} · degraded {summary.degraded}
            {matrix.generated_at != null && (
              <>
                {' '}
                · refreshed {formatAge(matrix.generated_at)}
              </>
            )}
            {context != null && (
              <>
                {' '}
                · spine phase {context.deployment.phase} / track {context.deployment.active_track}
              </>
            )}
          </p>
        )}
        {selectedNode != null && (
          <p className="m-0 mt-1 text-[var(--text-dense)]">
            Filtered for <strong>{selectedNode.label}</strong>
          </p>
        )}
        {failCount > 0 && !showAllLayers && (
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Showing {layers.length} layer(s) with failures.
          </p>
        )}
      </header>

      <div
        ref={listRef}
        className="runtime-scope-list flex flex-col gap-2 overflow-auto max-h-[520px]"
      >
        {layers.map(layer => (
          <ScopeLayerCard
            key={layer.tag}
            layer={layer}
            matrix={matrix}
            scopeGap={scopeGapMap.get(layer.tag)}
            selected={selection?.kind === 'scope' && selection.tag === layer.tag}
            selectedTargetId={selection?.kind === 'target' ? selection.id : null}
            forceOpen={
              selection?.kind === 'scope' && selection.tag === layer.tag
            }
            onSelectScope={() => onSelectScope(layer.tag)}
            onSelectTarget={onSelectTarget}
          />
        ))}
      </div>
    </section>
  )
}

function ScopeLayerCard({
  layer,
  matrix,
  scopeGap,
  selected,
  selectedTargetId,
  forceOpen,
  onSelectScope,
  onSelectTarget,
}: {
  layer: ScopeLayer
  matrix: MatrixResponse | undefined
  scopeGap?: ScopeGapSummary
  selected: boolean
  selectedTargetId: string | null
  forceOpen?: boolean
  onSelectScope: () => void
  onSelectTarget: (id: string) => void
}) {
  const [open, setOpen] = useState(true)
  const isOpen = forceOpen || open
  const targets = sortTargetsByReachability(
    matrix?.targets.filter(t => layer.targetIds.includes(t.id)) ?? [],
  )

  return (
    <div
      data-scope-tag={layer.tag}
      className={[
        'runtime-scope-card border border-[var(--border)] rounded-[var(--radius)]',
        selected ? 'runtime-scope-card--selected' : '',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <button
        type="button"
        className="runtime-scope-card-header w-full text-left px-3 py-2 flex items-start gap-2"
        onClick={() => {
          setOpen(v => !v)
          onSelectScope()
        }}
      >
        <ComponentIcon
          componentId={componentIdForScopeTag(layer.tag)}
          variant="scope"
          showWell
          className="shrink-0"
        />
        <span className="badge-ui font-mono-tabular shrink-0">{layer.tag}</span>
        <div className="min-w-0 flex-1">
          <div className="text-[var(--text-dense)] font-medium truncate">{layer.component}</div>
          <div className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] truncate">
            {layer.technology}
          </div>
        </div>
        {scopeGap != null && (
          <span
            className={`gap-badge gap-badge--${scopeGap.status === 'live' ? 'live' : scopeGap.status === 'partial' ? 'partial' : 'planned'} shrink-0`}
          >
            {scopeGap.status === 'live'
              ? 'LIVE'
              : scopeGap.status === 'partial'
                ? 'PARTIAL'
                : 'PLANNED'}
          </span>
        )}
        {scopeGap == null && layer.plannedOnly && (
          <span className="gap-badge gap-badge--planned shrink-0">PLANNED</span>
        )}
        <span className="text-[var(--muted-foreground)] text-xs shrink-0">{isOpen ? '−' : '+'}</span>
      </button>
      {isOpen && (
        <div className="px-3 pb-2">
          {targets.length > 0 ? (
            <ul className="m-0 list-none p-0 flex flex-col gap-1">
              {targets.map(t => (
                <li key={t.id} data-target-id={t.id}>
                  <button
                    type="button"
                    className={[
                      'runtime-target-row w-full text-left flex items-center gap-2 px-2 py-1 rounded',
                      selectedTargetId === t.id ? 'runtime-target-row--selected' : '',
                    ]
                      .filter(Boolean)
                      .join(' ')}
                    onClick={() => onSelectTarget(t.id)}
                  >
                    <StatusLamp value={t.reachability} kind="reach" />
                    <span className="font-mono-tabular text-[var(--text-dense)]">{t.id}</span>
                    <span className="text-[var(--muted-foreground)] text-[var(--text-dense-meta)] truncate flex-1">
                      {t.detail}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {layer.plannedOnly ? 'Planning target — no live matrix probes yet.' : 'No probes in this environment.'}
            </p>
          )}
          <p className="m-0 mt-2 text-[10px] text-[var(--muted-foreground)] line-clamp-2">{layer.notes}</p>
        </div>
      )}
    </div>
  )
}

function formatAge(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime()
  if (ms < 60_000) return 'just now'
  const min = Math.floor(ms / 60_000)
  if (min < 60) return `${min}m ago`
  return `${Math.floor(min / 60)}h ago`
}

export { matrixTargetToScopeTag }
