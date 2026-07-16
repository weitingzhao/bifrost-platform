import { useState } from 'react'
import {
  BriefingStatusBadge,
  BriefingStatusLamp,
} from '@/components/briefing/BriefingStatusChrome'
import type { DeliveryBoardProgramOverview } from '@/api/programsTypes'
import { laneById } from '@/lib/briefing/workLanes'

const PROGRAM_TAG_GRID = 'grid min-w-0 grid-cols-1 gap-2 sm:grid-cols-2'

export type DeliveryBoardCompleteGridProps = {
  programs: DeliveryBoardProgramOverview[]
  selectedProgramId: string | null
  onSelect: (programId: string) => void
  defaultExpanded?: boolean
}

/**
 * Complete programs — Tag Grid aligned with Briefing CompletedLanesGroup visuals.
 * Read-only catalog cards (sign-off lives in Briefing Session).
 */
export function DeliveryBoardCompleteGrid({
  programs,
  selectedProgramId,
  onSelect,
  defaultExpanded = true,
}: DeliveryBoardCompleteGridProps) {
  const [expanded, setExpanded] = useState(defaultExpanded)

  if (programs.length === 0) return null

  return (
    <div className="rounded-md border border-border/60 bg-secondary/20 px-2.5 py-2">
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left transition-colors hover:bg-secondary"
        onClick={() => setExpanded(v => !v)}
      >
        <BriefingStatusLamp status="done" />
        <span className="text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
          Complete
        </span>
        <BriefingStatusBadge status="done" label={`${programs.length}`} />
        <span className="text-dense-caption text-muted-foreground">
          Catalog · Sign-off in Briefing Session
        </span>
        <span className="ml-auto text-dense-caption text-muted-foreground">
          {expanded ? '▾' : '▸'}
        </span>
      </button>
      {expanded && (
        <div className={`mt-2 ${PROGRAM_TAG_GRID}`}>
          {programs.map(program => {
            const selected = selectedProgramId === program.id
            const laneLabel = program.laneId
              ? (laneById(program.laneId)?.shortLabel ?? program.laneId)
              : 'Unassigned'
            return (
              <button
                key={program.id}
                type="button"
                className={[
                  'flex w-full min-w-0 flex-col gap-1 rounded-lg border px-3 py-2.5 text-left transition-colors',
                  selected
                    ? 'border-primary bg-primary/8'
                    : 'border-border/60 bg-card/60 opacity-80 hover:opacity-100 hover:bg-secondary/40',
                ].join(' ')}
                onClick={() => onSelect(program.id)}
              >
                <div className="flex items-start justify-between gap-2">
                  <span className="text-dense-label font-medium text-foreground">{program.label}</span>
                  <BriefingStatusBadge status="done" label="Done" />
                </div>
                <p className="m-0 line-clamp-2 text-dense-caption text-muted-foreground">
                  {program.description}
                </p>
                <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-dense-caption text-muted-foreground">
                  <span className="font-mono">
                    {program.signed}/{program.phaseCount}
                  </span>
                  <span>·</span>
                  <span>{laneLabel}</span>
                  {program.formerLocation ? (
                    <>
                      <span>·</span>
                      <span className="truncate">{program.formerLocation}</span>
                    </>
                  ) : null}
                </div>
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}
