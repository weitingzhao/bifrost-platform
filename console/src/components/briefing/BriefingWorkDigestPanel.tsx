import { useMemo } from 'react'
import type { ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'
import {
  BriefingProgressMeter,
  BriefingStatusBadge,
  BriefingStatusLamp,
  briefingDigestTileClass,
} from '@/components/briefing/BriefingStatusChrome'
import {
  computeScopeWorkSummary,
  laneLifecycleFromQueue,
  briefingLifecycleFilterLabel,
  type BriefingLaneLifecycleFilter,
  type BriefingWorkStatus,
} from '@/lib/briefing/briefingStatus'
import { componentLineById, type ComponentLineId } from '@/lib/briefing/briefingViewTabs'
import { allWorkLanes, buildQueueForLane } from '@/lib/briefing/workLanes'

const LIFECYCLE_TILES: Array<{
  filter: BriefingLaneLifecycleFilter
  label: string
  countKey: 'doing' | 'planned' | 'ready' | 'done'
  status: BriefingWorkStatus
}> = [
  { filter: 'active', label: 'Doing', countKey: 'doing', status: 'doing' },
  { filter: 'planned', label: 'Planned', countKey: 'planned', status: 'planned' },
  { filter: 'empty', label: 'Ready', countKey: 'ready', status: 'ready' },
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
  /** Full-width summary strip (top of Briefing Master-Detail page). */
  compact?: boolean
}

function DigestTile({
  label,
  value,
  status,
  selected,
  onClick,
  compact,
}: {
  label: string
  value: string
  status: BriefingWorkStatus
  selected: boolean
  onClick: () => void
  compact?: boolean
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onClick}
      className={
        compact
          ? [
              'inline-flex h-8 items-center gap-1.5 rounded-md border px-2 text-left transition-colors',
              selected
                ? 'border-[var(--border)] border-l-2 border-l-[var(--primary)] bg-[var(--card)]'
                : 'border-[var(--border)]/50 bg-[var(--muted)]/30 opacity-75 hover:opacity-95',
            ].join(' ')
          : briefingDigestTileClass(selected)
      }
    >
      {compact ? (
        <>
          <BriefingStatusLamp status={status} />
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {label}
          </span>
          <span className="font-semibold tabular-nums text-[var(--text-dense-label)]">{value}</span>
        </>
      ) : (
        <>
          <div className="flex items-center gap-1.5">
            <BriefingStatusLamp status={status} />
            <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {label}
            </span>
          </div>
          <p className="m-0 mt-0.5 text-lg font-semibold tabular-nums leading-none">{value}</p>
        </>
      )}
    </button>
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
  compact = false,
}: BriefingWorkDigestPanelProps) {
  const { summary, hotLines, laneTotal } = useMemo(() => {
    const lanes = allWorkLanes()
    const queues = lanes.map(lane => ({
      label: lane.label,
      queue: buildQueueForLane(lane.id, context, matrices, clusterSummary),
      line: lane.componentLine,
    }))
    const summary = computeScopeWorkSummary(
      queues.map(({ label, queue }) => ({ label, queue })),
    )

    const doingByLine = new Map<ComponentLineId, number>()
    for (const { queue, line } of queues) {
      if (laneLifecycleFromQueue(queue) !== 'active') continue
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
  }, [context, matrices, clusterSummary])

  const { status, progress, nextStep, laneCounts } = summary
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
        </div>

        {loading === true && context == null && (
          <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Loading spine &amp; matrix…
          </p>
        )}

        <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
          {LIFECYCLE_TILES.map(tile => (
            <DigestTile
              key={tile.filter}
              label={tile.label}
              value={String(laneCounts[tile.countKey])}
              status={tile.status}
              selected={lifecycleFilter === tile.filter}
              onClick={() => toggleLifecycle(tile.filter)}
              compact
            />
          ))}
          {progress != null && (
            <div className="min-w-[7rem] flex-1 basis-[7rem]">
              <BriefingProgressMeter
                done={progress.done}
                total={progress.total}
                percent={progress.percent}
                status={status}
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
        Portfolio totals stay fixed. Click a status tile to filter Lanes below (All scope); click
        again to clear. Hot line chips jump to that line with Doing filter.
      </p>

      {loading === true && context == null && (
        <p className="mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading spine &amp; matrix…
        </p>
      )}

      <div className="mt-3 grid items-stretch gap-3 sm:grid-cols-2 lg:grid-cols-5">
        {LIFECYCLE_TILES.map(tile => (
          <DigestTile
            key={tile.filter}
            label={tile.label}
            value={String(laneCounts[tile.countKey])}
            status={tile.status}
            selected={lifecycleFilter === tile.filter}
            onClick={() => toggleLifecycle(tile.filter)}
          />
        ))}
        <button
          type="button"
          className={briefingDigestTileClass(false)}
          onClick={onClearFilters}
          title={hasActiveFilter ? 'Clear lifecycle filter' : 'Queue progress (portfolio)'}
        >
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Queue progress
          </span>
          {progress != null ? (
            <BriefingProgressMeter
              done={progress.done}
              total={progress.total}
              percent={progress.percent}
              status={status}
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
