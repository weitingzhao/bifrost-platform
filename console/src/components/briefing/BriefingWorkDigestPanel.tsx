import { useMemo } from 'react'
import { ChevronRight } from 'lucide-react'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import {
  BriefingLifecycleStackMeter,
  BriefingStatusBadge,
  BriefingStatusLamp,
} from '@/components/briefing/BriefingStatusChrome'
import { briefingDigestTileClass } from '@/components/briefing/briefingStatusChromeClasses'
import {
  computeScopeWorkSummary,
  isLaneLifecycleHold,
  laneLifecycleFromQueue,
  briefingLifecycleFilterLabel,
  type BriefingLaneLifecycleFilter,
} from '@/lib/briefing/briefingStatus'
import { componentLineById, type ComponentLineId } from '@/lib/briefing/briefingViewTabs'
import { allWorkLanes, buildQueueForLane } from '@/lib/briefing/workLanes'
import { useDeliveryProgramClosure } from '@/hooks/useDeliveryProgramClosure'

/** Maturity timeline: Ready → Planned → Doing → Done. */
const LIFECYCLE_TILES: Array<{
  filter: BriefingLaneLifecycleFilter
  label: string
  countKey: 'doing' | 'planned' | 'ready' | 'done'
  status: 'doing' | 'planned' | 'ready' | 'done'
}> = [
  { filter: 'empty', label: 'Ready', countKey: 'ready', status: 'ready' },
  { filter: 'planned', label: 'Planned', countKey: 'planned', status: 'planned' },
  { filter: 'active', label: 'Doing', countKey: 'doing', status: 'doing' },
  { filter: 'complete', label: 'Done', countKey: 'done', status: 'done' },
]

interface BriefingWorkDigestPanelProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
  loading?: boolean
  /** Active digest → lanes filter (portfolio truth; digest numbers ignore this). */
  lifecycleFilter: BriefingLaneLifecycleFilter | null
  onSelectLifecycleFilter: (filter: BriefingLaneLifecycleFilter | null) => void
  onSelectHotLine: (line: ComponentLineId) => void
  onClearFilters: () => void
  onFocusAllScope: () => void
  /** Open top-level New Lane dialog (describe-first, no Scope prerequisite). */
  onNewLane?: () => void
  /** Full-width summary strip (top of Briefing Master-Detail page). */
  compact?: boolean
}

type LaneCounts = {
  doing: number
  planned: number
  ready: number
  done: number
}

/**
 * Maturity pipeline — Ready › Planned › Doing › Done.
 * Click a stage to filter Lanes; selected stage is highlighted.
 */
function LifecycleStepper({
  laneCounts,
  lifecycleFilter,
  onToggle,
  compact,
}: {
  laneCounts: LaneCounts
  lifecycleFilter: BriefingLaneLifecycleFilter | null
  onToggle: (filter: BriefingLaneLifecycleFilter) => void
  compact?: boolean
}) {
  return (
    <div
      role="list"
      aria-label="Lane maturity pipeline: Ready, Planned, Doing, Done"
      className={[
        'inline-flex min-w-0 max-w-full flex-wrap items-stretch overflow-hidden rounded-md border border-[var(--border)]/60 bg-[var(--muted)]/20',
        compact ? 'p-0.5' : 'p-1',
      ].join(' ')}
    >
      {LIFECYCLE_TILES.map((tile, index) => {
        const selected = lifecycleFilter === tile.filter
        const isLast = index === LIFECYCLE_TILES.length - 1
        return (
          <div key={tile.filter} role="listitem" className="flex min-w-0 items-stretch">
            <button
              type="button"
              aria-pressed={selected}
              aria-current={selected ? 'step' : undefined}
              title={`${tile.label} — filter Lanes (click again to clear)`}
              onClick={() => onToggle(tile.filter)}
              className={[
                'inline-flex min-w-0 items-center gap-1.5 rounded-sm px-2 text-left transition-colors',
                compact ? 'h-7' : 'h-9 px-2.5',
                selected
                  ? 'bg-[var(--card)] text-[var(--foreground)] shadow-[inset_0_0_0_1px_var(--primary)]'
                  : 'text-[var(--muted-foreground)] hover:bg-[var(--secondary)]/60',
              ].join(' ')}
            >
              {selected ? (
                <BriefingStatusLamp status={tile.status} />
              ) : (
                <span
                  className="status-lamp status-lamp--filled text-[var(--muted-foreground)]"
                  aria-hidden
                >
                  ●
                </span>
              )}
              <span
                className={[
                  'shrink-0',
                  compact ? 'text-[var(--text-dense-caption)]' : 'text-[var(--text-dense-meta)]',
                  selected ? 'font-semibold' : 'font-medium',
                ].join(' ')}
              >
                {tile.label}
              </span>
              <span
                className={[
                  'tabular-nums',
                  compact ? 'text-[var(--text-dense-label)]' : 'text-[var(--text-dense-body)]',
                  selected ? 'font-semibold' : 'font-medium',
                ].join(' ')}
              >
                {laneCounts[tile.countKey]}
              </span>
            </button>
            {!isLast && (
              <span
                aria-hidden
                className="flex shrink-0 items-center px-0.5 text-[var(--muted-foreground)]/45"
              >
                <ChevronRight className={compact ? 'h-3.5 w-3.5' : 'h-4 w-4'} strokeWidth={2} />
              </span>
            )}
          </div>
        )
      })}
    </div>
  )
}

/**
 * Portfolio digest for Agent Briefing — lane-queue truth across every Work Scope.
 * Tiles filter the Lanes board below; digest counts always stay portfolio-wide.
 */
export function BriefingWorkDigestPanel({
  context,
  matrices,
  clusterSummary,
  loading,
  lifecycleFilter,
  onSelectLifecycleFilter,
  onSelectHotLine,
  onClearFilters,
  onFocusAllScope,
  onNewLane,
  compact = false,
}: BriefingWorkDigestPanelProps) {
  const { releasedByLane, programsReleasedFor, programs } = useDeliveryProgramClosure()
  const { summary, hotLines, laneTotal } = useMemo(() => {
    const lanes = allWorkLanes()
    const queues = lanes.map(lane => ({
      label: lane.label,
      laneId: lane.id,
      queue: buildQueueForLane(lane.id, context, matrices, clusterSummary, programs),
      line: lane.componentLine,
    }))
    const summary = computeScopeWorkSummary(
      queues.map(({ label, queue, laneId }) => ({ label, queue, laneId })),
      { programsReleasedByLane: releasedByLane },
    )

    const doingByLine = new Map<ComponentLineId, number>()
    for (const { queue, line, laneId } of queues) {
      const released = programsReleasedFor(laneId)
      if (isLaneLifecycleHold(queue, released)) continue
      if (laneLifecycleFromQueue(queue, { programsReleased: released }) !== 'active') {
        continue
      }
      doingByLine.set(line, (doingByLine.get(line) ?? 0) + 1)
    }
    const hotLines = [...doingByLine.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 3)
      .map(([lineId, count]) => ({
        lineId,
        label: componentLineById(lineId).shortLabel,
        count,
      }))

    return { summary, hotLines, laneTotal: lanes.length }
  }, [context, matrices, clusterSummary, releasedByLane, programsReleasedFor, programs])

  const { status, nextStep, laneCounts } = summary
  const hasActiveFilter = lifecycleFilter != null

  function toggleLifecycle(filter: BriefingLaneLifecycleFilter) {
    onSelectLifecycleFilter(lifecycleFilter === filter ? null : filter)
  }

  if (compact) {
    return (
      <section className="page-section panel-elevated px-3 py-2">
        <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5">
          <p className="briefing-section-kicker m-0 shrink-0">Summary</p>
          <BriefingStatusLamp status={status} />
          <BriefingStatusBadge status={status} />
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {laneTotal} lanes
          </span>
          {hasActiveFilter && (
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-md border border-[var(--primary)]/40 bg-[var(--primary)]/10 px-1.5 py-0.5 text-[var(--text-dense-caption)] font-medium"
              onClick={onClearFilters}
            >
              {briefingLifecycleFilterLabel(lifecycleFilter)} ✕
            </button>
          )}
          {onNewLane != null && (
            <button
              type="button"
              className="ml-auto inline-flex items-center gap-1 rounded-md border border-[var(--primary)]/45 bg-[var(--primary)]/10 px-2 py-0.5 text-[var(--text-dense-caption)] font-semibold text-[var(--primary)] transition-colors hover:bg-[var(--primary)]/18"
              onClick={onNewLane}
              title="Describe work first — recommend Line and Track Type"
            >
              + New Lane
            </button>
          )}
        </div>

        {loading === true && context == null && (
          <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Loading spine &amp; matrix…
          </p>
        )}

        <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-2">
          <LifecycleStepper
            laneCounts={laneCounts}
            lifecycleFilter={lifecycleFilter}
            onToggle={toggleLifecycle}
            compact
          />
          {laneTotal > 0 && (
            <div className="min-w-[7rem] flex-1 basis-[7rem]">
              <BriefingLifecycleStackMeter
                ready={laneCounts.ready}
                planned={laneCounts.planned}
                doing={laneCounts.doing}
                done={laneCounts.done}
                className="mt-0"
              />
            </div>
          )}
        </div>

        <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
          <p className="m-0 min-w-0 flex-1 truncate text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {nextStep != null ? (
              <>
                <span>Next: </span>
                <span className="text-[var(--foreground)]">{nextStep}</span>
              </>
            ) : (
              'Pick a Ready lane or add New Lane below.'
            )}
          </p>
          {hotLines.map(h => (
            <button
              key={h.lineId}
              type="button"
              className="rounded px-1 py-0.5 text-[var(--text-dense-caption)] font-medium uppercase tracking-wider bg-[color-mix(in_srgb,var(--color-lamp-yellow)_22%,transparent)] text-[var(--color-lamp-yellow)] transition-opacity hover:opacity-80"
              onClick={() => onSelectHotLine(h.lineId)}
              title={`Filter ${h.label} · Doing`}
            >
              {h.label} {h.count}
            </button>
          ))}
          <button
            type="button"
            className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)] underline-offset-2 hover:underline"
            onClick={onFocusAllScope}
          >
            All scope
          </button>
        </div>
      </section>
    )
  }

  return (
    <section className="page-section panel-elevated px-4 py-3">
      <div className="flex min-h-7 flex-wrap items-center gap-2">
        <p className="briefing-section-kicker m-0">Summary</p>
        <BriefingStatusLamp status={status} />
        <BriefingStatusBadge status={status} />
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {laneTotal} lanes catalogued
        </span>
        {hasActiveFilter ? (
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded-md border border-[var(--primary)]/40 bg-[var(--primary)]/10 px-2 py-0.5 text-[var(--text-dense-caption)] font-medium text-[var(--foreground)]"
            onClick={onClearFilters}
          >
            Filter · {briefingLifecycleFilterLabel(lifecycleFilter)} ✕
          </button>
        ) : (
          <span className="invisible inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[var(--text-dense-caption)] font-medium" aria-hidden>
            Filter · Doing ✕
          </span>
        )}
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Maturity pipeline (Ready → Planned → Doing → Done). Click a stage to filter Lanes; click
        again to clear. Hot line chips jump to that line with Doing filter.
      </p>

      {loading === true && context == null && (
        <p className="mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading spine &amp; matrix…
        </p>
      )}

      <div className="mt-3 flex min-w-0 flex-wrap items-center gap-3">
        <LifecycleStepper
          laneCounts={laneCounts}
          lifecycleFilter={lifecycleFilter}
          onToggle={toggleLifecycle}
        />
        <button
          type="button"
          className={`${briefingDigestTileClass(false)} min-w-[8rem] flex-1 basis-[8rem]`}
          onClick={onClearFilters}
          title={hasActiveFilter ? 'Clear lifecycle filter' : 'Lane maturity mix (Ready → Done)'}
        >
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Lane maturity
          </span>
          {laneTotal > 0 ? (
            <BriefingLifecycleStackMeter
              ready={laneCounts.ready}
              planned={laneCounts.planned}
              doing={laneCounts.doing}
              done={laneCounts.done}
              className="mt-1"
            />
          ) : (
            <p className="m-0 mt-1 text-lg font-semibold tabular-nums leading-none text-[var(--muted-foreground)]">
              —
            </p>
          )}
        </button>
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-[var(--text-dense-body)]">
          {nextStep != null ? (
            <>
              <span className="text-[var(--muted-foreground)]">Next: </span>
              {nextStep}
            </>
          ) : laneCounts.doing === 0 && laneCounts.planned === 0 ? (
            <span className="text-[var(--muted-foreground)]">
              No active Briefing work — pick a Ready lane or add a New Lane below.
            </span>
          ) : (
            <span className="text-[var(--muted-foreground)]">No next step derived.</span>
          )}
        </p>
        <div className="flex flex-wrap items-center gap-2">
          {hotLines.map(h => (
            <button
              key={h.lineId}
              type="button"
              className="rounded px-1.5 py-0.5 text-[var(--text-dense-caption)] font-medium uppercase tracking-wider bg-[color-mix(in_srgb,var(--color-lamp-yellow)_22%,transparent)] text-[var(--color-lamp-yellow)] transition-opacity hover:opacity-80"
              onClick={() => onSelectHotLine(h.lineId)}
              title={`Filter ${h.label} · Doing`}
            >
              {h.label} · {h.count} doing
            </button>
          ))}
          <button
            type="button"
            className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)] underline-offset-2 hover:underline"
            onClick={onFocusAllScope}
          >
            Open All scope →
          </button>
        </div>
      </div>
    </section>
  )
}
