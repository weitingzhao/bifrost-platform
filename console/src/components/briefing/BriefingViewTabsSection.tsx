import { useMemo } from 'react'
import { LayoutGrid } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import {
  BRIEFING_DPR_COLOR,
  BriefingProgressMeter,
  BriefingStatusBadge,
  BriefingStatusLamp,
  briefingScopeGridCellClass,
  briefingTrackTypeCardClass,
} from '@/components/briefing/BriefingStatusChrome'
import {
  briefingLifecycleFilterLabel,
  laneLifecycleFromQueue,
  type ScopeWorkSummary,
  type BriefingLaneLifecycleFilter,
} from '@/lib/briefing/briefingStatus'
import {
  COMPONENT_LINE_DEFS,
  briefingScopeById,
  lanesForScopeTrack,
  taskModeForBriefingScope,
  trackTypeById,
  trackTypeDefsForScope,
  type BriefingScopeId,
  type ComponentLineId,
  type WorkTrackType,
  type WorkTrackTypeDef,
} from '@/lib/briefing/briefingViewTabs'
import { buildQueueForLane } from '@/lib/briefing/workLanes'
import type { TaskModeId } from '@/lib/task-mode/types'
import type { ClusterSummary, MatrixResponse, OpsContextResponse } from '@/api/types'

/** Active-work counts for Briefing scope tags — Done omitted (Delivery Board concern). */
export type ScopeActiveCounts = {
  doing: number
  planned: number
  ready: number
}

function countActiveLanes(
  scope: BriefingScopeId,
  trackType: WorkTrackType,
  context: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
  clusterSummary: ClusterSummary | undefined,
): ScopeActiveCounts {
  const counts: ScopeActiveCounts = { doing: 0, planned: 0, ready: 0 }
  for (const lane of lanesForScopeTrack(scope, trackType)) {
    const life = laneLifecycleFromQueue(
      buildQueueForLane(lane.id, context, matrices, clusterSummary),
    )
    if (life === 'active') counts.doing += 1
    else if (life === 'planned') counts.planned += 1
    else if (life === 'empty') counts.ready += 1
  }
  return counts
}

function formatDPR(c: ScopeActiveCounts): string {
  return `${c.doing}/${c.planned}/${c.ready}`
}

function totalActive(c: ScopeActiveCounts): number {
  return c.doing + c.planned + c.ready
}

/** Colored d/p/r digits — zero stays muted so non-zero status pops. */
function ScopeDprCounts({ counts }: { counts: ScopeActiveCounts }) {
  const sep = 'text-[var(--muted-foreground)]/40'
  const digit = (n: number, tone: keyof typeof BRIEFING_DPR_COLOR) =>
    n > 0 ? BRIEFING_DPR_COLOR[tone] : 'text-[var(--muted-foreground)]/45'

  return (
    <span className="font-mono text-[var(--text-dense-micro)] leading-none tabular-nums tracking-tight">
      <span className={digit(counts.doing, 'doing')}>{counts.doing}</span>
      <span className={sep}>/</span>
      <span className={digit(counts.planned, 'planned')}>{counts.planned}</span>
      <span className={sep}>/</span>
      <span className={digit(counts.ready, 'ready')}>{counts.ready}</span>
    </span>
  )
}

/**
 * Compact grid cell for Scope Line picker — icon · name · d/p/r in one row.
 * Selected accent matches the View theme via `data-task-mode` CSS var.
 */
function ScopeGridCell({
  label,
  icon: Icon,
  selected,
  counts,
  onSelect,
  span2,
  taskMode,
}: {
  label: string
  icon: LucideIcon
  selected: boolean
  counts: ScopeActiveCounts
  onSelect: () => void
  span2?: boolean
  taskMode: TaskModeId
}) {
  const quiet = !selected && totalActive(counts) === 0
  return (
    <button
      type="button"
      aria-pressed={selected}
      data-task-mode={taskMode}
      title={`${label}: ${formatDPR(counts)} · Doing / Planned / Ready`}
      className={[
        briefingScopeGridCellClass(selected, span2),
        quiet ? 'opacity-55' : '',
      ].join(' ')}
      onClick={onSelect}
    >
      <Icon
        className={[
          'h-3 w-3 shrink-0 transition-colors',
          selected ? 'text-[var(--task-mode-accent)]' : 'text-[var(--muted-foreground)]',
        ].join(' ')}
        strokeWidth={2}
        aria-hidden
      />
      <span
        className={[
          'min-w-0 truncate text-[var(--text-dense-caption)] transition-colors',
          selected
            ? 'font-semibold text-[var(--task-mode-accent)]'
            : 'font-medium text-[var(--muted-foreground)]',
        ].join(' ')}
      >
        {label}
      </span>
      <span className="ml-auto">
        <ScopeDprCounts counts={counts} />
      </span>
    </button>
  )
}

function TrackTypeCard({
  def,
  selected,
  laneCount,
  onSelect,
}: {
  def: WorkTrackTypeDef
  selected: boolean
  laneCount: number
  onSelect: () => void
}) {
  const Icon = def.icon
  return (
    <button
      type="button"
      className={briefingTrackTypeCardClass(selected)}
      onClick={onSelect}
    >
      <span
        className={[
          'flex h-6 w-6 shrink-0 items-center justify-center rounded-md',
          selected
            ? 'bg-[var(--foreground)]/10 text-[var(--foreground)]'
            : 'bg-[var(--border)]/60 text-[var(--muted-foreground)]',
        ].join(' ')}
      >
        <Icon className="h-3.5 w-3.5" strokeWidth={2} aria-hidden />
      </span>
      <span className="min-w-0 flex-1 truncate text-[var(--text-dense-label)] font-semibold">
        {def.label}
      </span>
      <span className="shrink-0 font-mono text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        {laneCount} {laneCount === 1 ? 'lane' : 'lanes'}
      </span>
    </button>
  )
}

export type BriefingViewTabsSectionProps = {
  selectedScope: BriefingScopeId
  selectedTrackType: WorkTrackType
  onSelectScope: (scope: BriefingScopeId) => void
  onSelectTrackType: (tt: WorkTrackType) => void
  scopeWorkSummary: ScopeWorkSummary
  lifecycleFilter?: BriefingLaneLifecycleFilter | null
  onClearLifecycleFilter?: () => void
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
}

export function BriefingViewTabsSection({
  selectedScope,
  selectedTrackType,
  onSelectScope,
  onSelectTrackType,
  scopeWorkSummary,
  lifecycleFilter = null,
  onClearLifecycleFilter,
  context,
  matrices,
  clusterSummary,
}: BriefingViewTabsSectionProps) {
  const scopeDef = briefingScopeById(selectedScope)
  const trackTypeDefs = trackTypeDefsForScope(selectedScope)
  const ttDef = trackTypeById(selectedTrackType)
  const singleTrackType = trackTypeDefs.length <= 1
  const isAll = selectedScope === 'all'
  const { status, progress, nextStep, laneCounts } = scopeWorkSummary
  const filterLabel =
    lifecycleFilter != null ? briefingLifecycleFilterLabel(lifecycleFilter) : null

  const scopeCounts = useMemo(() => {
    const byLine = Object.fromEntries(
      COMPONENT_LINE_DEFS.map(line => [
        line.id,
        countActiveLanes(line.id, selectedTrackType, context, matrices, clusterSummary),
      ]),
    ) as Record<ComponentLineId, ScopeActiveCounts>
    const all = countActiveLanes('all', selectedTrackType, context, matrices, clusterSummary)
    return { byLine, all }
  }, [selectedTrackType, context, matrices, clusterSummary])

  return (
    <section className="page-section panel-elevated px-3 py-2">
      <div className="min-w-0">
        <p className="briefing-section-kicker m-0">Scope</p>
        <h2 className="m-0 mt-0.5 text-sm font-semibold">Work scope &amp; lines</h2>
      </div>

      {/* Zone A — 2-col scope grid */}
      <div className="mt-1.5">
        <div className="mb-1 flex items-baseline justify-between gap-2">
          <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
            Line
          </span>
          <p className="m-0 flex items-center gap-0.5 font-mono text-[var(--text-dense-micro)] leading-none">
            <span className={BRIEFING_DPR_COLOR.doing}>d</span>
            <span className="text-[var(--muted-foreground)]/40">/</span>
            <span className={BRIEFING_DPR_COLOR.planned}>p</span>
            <span className="text-[var(--muted-foreground)]/40">/</span>
            <span className={BRIEFING_DPR_COLOR.ready}>r</span>
          </p>
        </div>

        <div className="grid grid-cols-2 gap-px rounded-md border border-[var(--border)]/60 bg-[var(--border)]/40 overflow-hidden">
          <ScopeGridCell
            label="All"
            icon={LayoutGrid}
            selected={isAll}
            counts={scopeCounts.all}
            onSelect={() => onSelectScope('all')}
            span2
            taskMode={taskModeForBriefingScope('all')}
          />
          {COMPONENT_LINE_DEFS.map(line => (
            <ScopeGridCell
              key={line.id}
              label={line.shortLabel}
              icon={line.icon}
              selected={!isAll && selectedScope === line.id}
              counts={scopeCounts.byLine[line.id]}
              onSelect={() => onSelectScope(line.id)}
              taskMode={taskModeForBriefingScope(line.id)}
            />
          ))}
        </div>
      </div>

      {/* Divider — Scope → Track type */}
      <div className="my-1.5 flex items-center gap-2">
        <div className="h-px flex-1 bg-[var(--border)]" />
        <span className="shrink-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wider text-[var(--muted-foreground)]">
          Track type
        </span>
        <div className="h-px flex-1 bg-[var(--border)]" />
      </div>

      {/* Zone B — Track Type cards (Layer 2) */}
      <div>
        {!singleTrackType ? (
          <div
            className="grid gap-1.5"
            style={{
              gridTemplateColumns: `repeat(${Math.min(trackTypeDefs.length, 2)}, minmax(0, 1fr))`,
            }}
          >
            {trackTypeDefs.map(def => (
              <TrackTypeCard
                key={def.id}
                def={def}
                selected={selectedTrackType === def.id}
                laneCount={lanesForScopeTrack(selectedScope, def.id).length}
                onSelect={() => onSelectTrackType(def.id)}
              />
            ))}
          </div>
        ) : (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            This scope has a single track type: <strong>{ttDef.label}</strong>
          </p>
        )}

        {/* Summary strip — compact for master pane */}
        <div className="mt-2 flex flex-wrap items-start gap-2 rounded-md border border-border bg-[var(--card)] px-2.5 py-2">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <div className="flex flex-wrap items-center gap-1.5">
              <BriefingStatusLamp status={status} />
              <span className="text-[var(--text-dense-label)] font-semibold">{scopeDef.label}</span>
              <span className="rounded bg-border px-1.5 py-0.5 text-[var(--text-dense-caption)] font-medium uppercase tracking-wider text-muted-foreground">
                {ttDef.label}
              </span>
              {isAll && <BriefingStatusBadge status="ready" label="Aggregate" />}
              <BriefingStatusBadge status={status} />
              {filterLabel != null && (
                <button
                  type="button"
                  className="inline-flex items-center gap-1 rounded-md border border-[var(--primary)]/40 bg-[var(--primary)]/10 px-2 py-0.5 text-[var(--text-dense-caption)] font-medium"
                  onClick={() => onClearLifecycleFilter?.()}
                >
                  Filter · {filterLabel} ✕
                </button>
              )}
            </div>
            <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
              {laneCounts.doing} doing · {laneCounts.planned} planned · {laneCounts.ready} ready
            </p>
            {nextStep != null && nextStep !== '' ? (
              <p className="m-0 truncate text-[var(--text-dense-caption)]">
                <span className="text-muted-foreground">Next: </span>
                <span className="text-foreground">{nextStep}</span>
              </p>
            ) : status === 'done' ? (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                All lanes complete — start a new lane.
              </p>
            ) : status === 'ready' ? (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                No active queue — pick a ready lane.
              </p>
            ) : null}
          </div>
          {progress != null && (
            <div className="w-24 shrink-0">
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
      </div>
    </section>
  )
}
