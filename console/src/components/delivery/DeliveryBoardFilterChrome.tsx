import { Button } from '@bifrost/ui'
import { LayoutGrid, type LucideIcon } from 'lucide-react'
import {
  BRIEFING_DPR_COLOR,
  briefingScopeGridCellClass,
  briefingTrackTypeCardClass,
} from '@/components/briefing/BriefingStatusChrome'
import {
  COMPONENT_LINE_DEFS,
  briefingScopeById,
  lanesForScope,
  lanesForScopeTrack,
  trackTypeById,
  trackTypeDefsForScope,
  type BriefingScopeId,
  type ComponentLineId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import { type LaneId } from '@/lib/briefing/workLanes'

/** Per-scope Delivery Board bands — not started / in progress / done. */
export type DeliveryScopeBandCounts = {
  notStarted: number
  inProgress: number
  complete: number
}

export type DeliveryBoardFilterChromeProps = {
  scope: BriefingScopeId
  trackType: WorkTrackType | null
  laneId: LaneId | null
  onScopeChange: (scope: BriefingScopeId) => void
  onTrackTypeChange: (tt: WorkTrackType | null) => void
  onLaneChange: (laneId: LaneId | null) => void
  /** Lane ids that have at least one board-visible program (optional highlight). */
  lanesWithPrograms?: ReadonlySet<string>
  /** Band counts per scope (All + each component line). */
  scopeBandCounts?: {
    all: DeliveryScopeBandCounts
    byLine: Record<ComponentLineId, DeliveryScopeBandCounts>
  }
}

const EMPTY_BANDS: DeliveryScopeBandCounts = {
  notStarted: 0,
  inProgress: 0,
  complete: 0,
}

function bandTotal(c: DeliveryScopeBandCounts): number {
  return c.notStarted + c.inProgress + c.complete
}

function formatBandCounts(c: DeliveryScopeBandCounts): string {
  return `${c.notStarted}/${c.inProgress}/${c.complete}`
}

/** Compact ns/prog/done digits — zero muted; non-zero uses band colors. */
function ScopeBandCounts({ counts }: { counts: DeliveryScopeBandCounts }) {
  const sep = 'text-muted-foreground/40'
  const digit = (n: number, tone: 'notStarted' | 'inProgress' | 'complete') => {
    if (n <= 0) return 'text-muted-foreground/45'
    if (tone === 'notStarted') return 'text-muted-foreground'
    if (tone === 'inProgress') return BRIEFING_DPR_COLOR.doing
    return 'text-success'
  }
  return (
    <span className="font-mono text-dense-micro leading-none tabular-nums tracking-tight">
      <span className={digit(counts.notStarted, 'notStarted')}>{counts.notStarted}</span>
      <span className={sep}>/</span>
      <span className={digit(counts.inProgress, 'inProgress')}>{counts.inProgress}</span>
      <span className={sep}>/</span>
      <span className={digit(counts.complete, 'complete')}>{counts.complete}</span>
    </span>
  )
}

function ScopeGridCell({
  label,
  icon: Icon,
  selected,
  counts,
  onSelect,
  span2,
  description,
}: {
  label: string
  icon: LucideIcon
  selected: boolean
  counts: DeliveryScopeBandCounts
  onSelect: () => void
  span2?: boolean
  description?: string
}) {
  const quiet = !selected && bandTotal(counts) === 0
  return (
    <button
      type="button"
      aria-pressed={selected}
      title={`${description ?? label}: ${formatBandCounts(counts)} · not started / in progress / done`}
      className={[
        briefingScopeGridCellClass(selected, span2),
        selected ? 'text-foreground' : 'text-muted-foreground',
        quiet ? 'opacity-55' : '',
      ].join(' ')}
      onClick={onSelect}
    >
      <Icon
        className={[
          'h-3.5 w-3.5 shrink-0 transition-colors',
          selected ? 'text-[var(--task-mode-accent)]' : '',
        ].join(' ')}
        aria-hidden
      />
      <span
        className={[
          'min-w-0 truncate text-dense-label transition-colors',
          selected ? 'font-semibold' : 'font-medium',
        ].join(' ')}
      >
        {label}
      </span>
      <span className="ml-auto shrink-0">
        <ScopeBandCounts counts={counts} />
      </span>
    </button>
  )
}

/**
 * Scope → Track Type → Lane filter chrome aligned with Agent Briefing taxonomy.
 * Does not mirror Briefing Done queues — only classifies Delivery programs.
 */
export function DeliveryBoardFilterChrome({
  scope,
  trackType,
  laneId,
  onScopeChange,
  onTrackTypeChange,
  onLaneChange,
  lanesWithPrograms,
  scopeBandCounts,
}: DeliveryBoardFilterChromeProps) {
  const scopeDef = briefingScopeById(scope)
  const trackDefs = trackTypeDefsForScope(scope)
  const lanes =
    trackType != null ? lanesForScopeTrack(scope, trackType) : lanesForScope(scope)
  const hasFilter = scope !== 'all' || trackType != null || laneId != null
  const allCounts = scopeBandCounts?.all ?? EMPTY_BANDS

  return (
    <div className="flex flex-col gap-2 rounded-md border border-border bg-secondary/20 px-2.5 py-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
          Scope → Track → Lane
        </p>
        {hasFilter && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              onScopeChange('all')
              onTrackTypeChange(null)
              onLaneChange(null)
            }}
          >
            Clear filter
          </Button>
        )}
      </div>

      <div className="flex items-baseline justify-between gap-2">
        <span className="text-dense-caption font-medium uppercase tracking-wider text-muted-foreground">
          Scope
        </span>
        <p
          className="m-0 flex items-center gap-0.5 font-mono text-dense-micro leading-none"
          title="not started / in progress / done — programs in each scope"
        >
          <span className="text-muted-foreground">ns</span>
          <span className="text-muted-foreground/40">/</span>
          <span className={BRIEFING_DPR_COLOR.doing}>prog</span>
          <span className="text-muted-foreground/40">/</span>
          <span className="text-success">done</span>
        </p>
      </div>

      <div
        className="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border"
        role="group"
        aria-label="Delivery Board scope"
      >
        <ScopeGridCell
          label="All"
          icon={LayoutGrid}
          selected={scope === 'all'}
          counts={allCounts}
          span2
          description="All component lines"
          onSelect={() => {
            onScopeChange('all')
            onTrackTypeChange(null)
            onLaneChange(null)
          }}
        />
        {COMPONENT_LINE_DEFS.map(line => (
          <ScopeGridCell
            key={line.id}
            label={line.shortLabel}
            icon={line.icon}
            selected={scope === line.id}
            counts={scopeBandCounts?.byLine[line.id] ?? EMPTY_BANDS}
            description={line.description}
            onSelect={() => {
              onScopeChange(line.id)
              onTrackTypeChange(null)
              onLaneChange(null)
            }}
          />
        ))}
      </div>

      {trackDefs.length > 0 && (
        <div className="flex flex-wrap gap-1.5" role="group" aria-label="Delivery Board track type">
          <button
            type="button"
            className={briefingTrackTypeCardClass(trackType == null)}
            onClick={() => {
              onTrackTypeChange(null)
              onLaneChange(null)
            }}
          >
            <span className="text-dense-caption font-medium">All tracks</span>
          </button>
          {trackDefs.map(tt => {
            const Icon = tt.icon
            const active = trackType === tt.id
            return (
              <button
                key={tt.id}
                type="button"
                className={briefingTrackTypeCardClass(active)}
                onClick={() => {
                  onTrackTypeChange(tt.id)
                  onLaneChange(null)
                }}
                title={tt.description}
              >
                <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
                <span className="text-dense-caption font-medium">{tt.shortLabel}</span>
              </button>
            )
          })}
        </div>
      )}

      <p className="m-0 text-dense-caption text-muted-foreground">
        {scopeDef.label}
        {trackType != null ? (
          <>
            {' '}
            · {trackTypeById(trackType).shortLabel}
          </>
        ) : null}
        {laneId != null ? (
          <>
            {' '}
            · lane{' '}
            <span className="font-mono text-foreground">{laneId}</span>
          </>
        ) : null}
      </p>

      <div className="flex flex-wrap gap-1.5" role="group" aria-label="Delivery Board lanes">
        <button
          type="button"
          className={[
            'rounded-md border px-2 py-1 text-dense-caption font-medium transition-colors',
            laneId == null
              ? 'border-border border-b-2 border-b-primary bg-card text-foreground'
              : 'border-border/50 bg-muted/30 text-muted-foreground hover:bg-secondary',
          ].join(' ')}
          onClick={() => onLaneChange(null)}
        >
          All lanes
        </button>
        {lanes.map(lane => {
          const active = laneId === lane.id
          const linked = lanesWithPrograms?.has(lane.id) ?? false
          return (
            <button
              key={lane.id}
              type="button"
              className={[
                'rounded-md border px-2 py-1 text-dense-caption font-medium transition-colors',
                active
                  ? 'border-border border-b-2 border-b-primary bg-card text-foreground'
                  : linked
                    ? 'border-border/50 bg-muted/20 text-foreground hover:bg-secondary'
                    : 'border-border/40 bg-muted/10 text-muted-foreground/70 hover:bg-secondary/60',
              ].join(' ')}
              onClick={() => onLaneChange(lane.id)}
              title={lane.description}
            >
              {lane.shortLabel}
            </button>
          )
        })}
      </div>
    </div>
  )
}
