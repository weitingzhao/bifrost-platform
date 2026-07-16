import { Button } from '@bifrost/ui'
import { LayoutGrid } from 'lucide-react'
import {
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
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import { type LaneId } from '@/lib/briefing/workLanes'

export type DeliveryBoardFilterChromeProps = {
  scope: BriefingScopeId
  trackType: WorkTrackType | null
  laneId: LaneId | null
  onScopeChange: (scope: BriefingScopeId) => void
  onTrackTypeChange: (tt: WorkTrackType | null) => void
  onLaneChange: (laneId: LaneId | null) => void
  /** Lane ids that have at least one board-visible program (optional highlight). */
  lanesWithPrograms?: ReadonlySet<string>
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
}: DeliveryBoardFilterChromeProps) {
  const scopeDef = briefingScopeById(scope)
  const trackDefs = trackTypeDefsForScope(scope)
  const lanes =
    trackType != null ? lanesForScopeTrack(scope, trackType) : lanesForScope(scope)
  const hasFilter = scope !== 'all' || trackType != null || laneId != null

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

      <div
        className="grid grid-cols-2 gap-px overflow-hidden rounded-md border border-border bg-border"
        role="group"
        aria-label="Delivery Board scope"
      >
        <button
          type="button"
          className={briefingScopeGridCellClass(scope === 'all', true)}
          onClick={() => {
            onScopeChange('all')
            onTrackTypeChange(null)
            onLaneChange(null)
          }}
        >
          <LayoutGrid className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <span className="text-dense-label font-medium">All</span>
        </button>
        {COMPONENT_LINE_DEFS.map(line => {
          const Icon = line.icon
          const active = scope === line.id
          return (
            <button
              key={line.id}
              type="button"
              className={briefingScopeGridCellClass(active)}
              onClick={() => {
                onScopeChange(line.id)
                onTrackTypeChange(null)
                onLaneChange(null)
              }}
              title={line.description}
            >
              <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
              <span className="truncate text-dense-label font-medium">{line.shortLabel}</span>
            </button>
          )
        })}
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
