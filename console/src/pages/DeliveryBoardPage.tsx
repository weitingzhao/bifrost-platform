import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  type DenseTagVariant,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { fetchDeliveryBoardPrograms, PROGRAMS_BOARD_QUERY_KEY } from '@/api/programs'
import { mapProgramSummaryToOverview } from '@/api/programsTypes'
import { BriefingStatusBadge, BriefingStatusLamp } from '@/components/briefing/BriefingStatusChrome'
import { DeliveryBoardCompleteGrid } from '@/components/delivery/DeliveryBoardCompleteGrid'
import {
  DeliveryBoardFilterChrome,
  type DeliveryScopeBandCounts,
} from '@/components/delivery/DeliveryBoardFilterChrome'
import { DeliveryBoardHistoricalArchive } from '@/components/delivery/DeliveryBoardHistoricalArchive'
import { DeliveryBoardProgramPanels } from '@/components/delivery/DeliveryBoardProgramPanels'
import { OpsSection } from '@/components/layout/OpsSection'
import { OpsVerdictStrip } from '@/components/layout/OpsVerdictStrip'
import {
  COMPONENT_LINE_DEFS,
  isBriefingScopeId,
  isWorkTrackType,
  lanesForScope,
  lanesForScopeTrack,
  type BriefingScopeId,
  type ComponentLineId,
  type WorkTrackType,
} from '@/lib/briefing/briefingViewTabs'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { laneById, type LaneId } from '@/lib/briefing/workLanes'

function programStatusVariant(signed: number, complete: boolean): DenseTagVariant {
  if (complete) return 'success'
  if (signed > 0) return 'warning'
  return 'neutral'
}

function filtersFromHash(): {
  scope: BriefingScopeId
  trackType: WorkTrackType | null
  laneId: LaneId | null
} {
  if (typeof window === 'undefined') return { scope: 'all', trackType: null, laneId: null }
  const hash = window.location.hash.replace(/^#/, '')
  const qIdx = hash.indexOf('?')
  if (qIdx < 0) return { scope: 'all', trackType: null, laneId: null }
  const params = new URLSearchParams(hash.slice(qIdx + 1))
  const lane = params.get('lane_id')
  const scopeRaw = params.get('scope')
  const ttRaw = params.get('tt')
  const laneId = lane != null && lane !== '' ? lane : null
  let scope: BriefingScopeId = 'all'
  let trackType: WorkTrackType | null = null
  if (scopeRaw != null && isBriefingScopeId(scopeRaw)) {
    scope = scopeRaw
  } else if (laneId != null) {
    const laneDef = laneById(laneId)
    if (laneDef.componentLine) scope = laneDef.componentLine
  }
  if (ttRaw != null && isWorkTrackType(ttRaw)) {
    trackType = ttRaw
  } else if (laneId != null) {
    trackType = laneById(laneId).trackType
  }
  return { scope, trackType, laneId }
}

function writeFiltersToHash(
  scope: BriefingScopeId,
  trackType: WorkTrackType | null,
  laneId: LaneId | null,
) {
  const params = new URLSearchParams()
  if (scope !== 'all') params.set('scope', scope)
  if (trackType != null) params.set('tt', trackType)
  if (laneId != null) params.set('lane_id', laneId)
  const q = params.toString()
  const base = 'delivery-board'
  window.location.hash = q ? `${base}?${q}` : base
}

function ProgramBandTable({
  programs,
  selectedProgramId,
  onSelect,
}: {
  programs: ReturnType<typeof mapProgramSummaryToOverview>[]
  selectedProgramId: string | null
  onSelect: (id: string) => void
}) {
  if (programs.length === 0) {
    return <p className="m-0 text-dense-meta text-muted-foreground">None</p>
  }
  return (
    <DenseDataTable>
      <DenseTableHeader>
        <DenseTableHeadRow>
          <DenseTableHead>Program</DenseTableHead>
          <DenseTableHead>Lane</DenseTableHead>
          <DenseTableHead>Done</DenseTableHead>
          <DenseTableHead>Signed</DenseTableHead>
          <DenseTableHead>Status</DenseTableHead>
          <DenseTableHead>Former location</DenseTableHead>
        </DenseTableHeadRow>
      </DenseTableHeader>
      <DenseTableBody>
        {programs.map(program => {
          const selected = selectedProgramId === program.id
          return (
            <DenseTableRow
              key={program.id}
              className={selected ? 'bg-secondary/40' : 'cursor-pointer hover:bg-secondary/20'}
              onClick={() => onSelect(program.id)}
            >
              <DenseTableCell>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">{program.label}</span>
                  <span className="text-dense-meta text-muted-foreground">{program.description}</span>
                </div>
              </DenseTableCell>
              <DenseTableCell className="font-mono text-dense-meta">
                {program.laneId ?? '—'}
              </DenseTableCell>
              <DenseTableCell className="font-mono-tabular">
                {program.phasesDone}/{program.phaseCount}
              </DenseTableCell>
              <DenseTableCell className="font-mono-tabular">
                {program.signed}/{program.gateCount} gates
              </DenseTableCell>
              <DenseTableCell>
                <DenseTag variant={programStatusVariant(program.signed, program.complete)}>
                  {program.complete ? 'Complete' : program.signed > 0 ? 'In progress' : 'Not started'}
                </DenseTag>
              </DenseTableCell>
              <DenseTableCell className="text-dense-meta text-muted-foreground">
                {program.formerLocation}
              </DenseTableCell>
            </DenseTableRow>
          )
        })}
      </DenseTableBody>
    </DenseDataTable>
  )
}

export function DeliveryBoardPage({
  onOpenBriefing,
  onOpenActiveSession,
}: {
  onOpenBriefing?: (opts?: BriefingUrlState) => void
  onOpenActiveSession?: (opts?: { laneId?: LaneId }) => void
} = {}) {
  const initial = filtersFromHash()
  const [scope, setScope] = useState<BriefingScopeId>(initial.scope)
  const [trackType, setTrackType] = useState<WorkTrackType | null>(initial.trackType)
  const [laneFilter, setLaneFilter] = useState<LaneId | null>(initial.laneId)
  const [selectedProgramId, setSelectedProgramId] = useState<string | null>(null)

  const programsQuery = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const allPrograms = useMemo(
    () => (programsQuery.data?.programs ?? []).map(mapProgramSummaryToOverview),
    [programsQuery.data],
  )

  const lanesWithPrograms = useMemo(() => {
    const set = new Set<string>()
    for (const p of allPrograms) {
      if (p.laneId) set.add(p.laneId)
    }
    return set
  }, [allPrograms])

  const scopeBandCounts = useMemo(() => {
    const bandOf = (p: (typeof allPrograms)[number]): keyof DeliveryScopeBandCounts => {
      if (p.complete) return 'complete'
      if (p.signed > 0) return 'inProgress'
      return 'notStarted'
    }
    const tally = (list: typeof allPrograms): DeliveryScopeBandCounts => {
      const c: DeliveryScopeBandCounts = { notStarted: 0, inProgress: 0, complete: 0 }
      for (const p of list) c[bandOf(p)] += 1
      return c
    }
    const byLine = Object.fromEntries(
      COMPONENT_LINE_DEFS.map(line => {
        const laneIds = new Set(lanesForScope(line.id).map(l => l.id))
        return [
          line.id,
          tally(allPrograms.filter(p => p.laneId != null && laneIds.has(p.laneId))),
        ]
      }),
    ) as Record<ComponentLineId, DeliveryScopeBandCounts>
    return { all: tally(allPrograms), byLine }
  }, [allPrograms])

  const programs = useMemo(() => {
    const laneIds = new Set(
      (trackType != null ? lanesForScopeTrack(scope, trackType) : lanesForScope(scope)).map(
        l => l.id,
      ),
    )
    return allPrograms.filter(p => {
      if (laneFilter != null) return p.laneId === laneFilter
      if (scope === 'all' && trackType == null) return true
      return p.laneId != null && laneIds.has(p.laneId)
    })
  }, [allPrograms, scope, trackType, laneFilter])

  const bands = useMemo(() => {
    const complete = programs.filter(p => p.complete)
    const inProgress = programs.filter(p => !p.complete && p.signed > 0)
    const notStarted = programs.filter(p => !p.complete && p.signed <= 0)
    return { complete, inProgress, notStarted }
  }, [programs])

  const selectedProgram = programs.find(p => p.id === selectedProgramId)

  const handleScopeChange = (next: BriefingScopeId) => {
    setScope(next)
    setTrackType(null)
    setLaneFilter(null)
    writeFiltersToHash(next, null, null)
  }

  const handleTrackTypeChange = (next: WorkTrackType | null) => {
    setTrackType(next)
    setLaneFilter(null)
    writeFiltersToHash(scope, next, null)
  }

  const handleLaneChange = (next: LaneId | null) => {
    setLaneFilter(next)
    writeFiltersToHash(scope, trackType, next)
  }

  const toggleSelect = (id: string) => {
    setSelectedProgramId(prev => (prev === id ? null : id))
  }

  const isLoading = programsQuery.isLoading
  const isError = programsQuery.isError
  const verdictLamp =
    isLoading || isError
      ? ('unknown' as const)
      : bands.inProgress.length > 0
        ? ('degraded' as const)
        : programs.length === 0
          ? ('unknown' as const)
          : bands.notStarted.length === programs.length
            ? ('unknown' as const)
            : ('ok' as const)
  const verdictTag: DenseTagVariant =
    isLoading || isError
      ? 'neutral'
      : bands.inProgress.length > 0
        ? 'warning'
        : programs.length > 0 && bands.complete.length === programs.length
          ? 'success'
          : 'neutral'
  const verdictLabel = isLoading
    ? 'PROBING'
    : isError
      ? 'ERROR'
      : programs.length === 0
        ? 'EMPTY'
        : bands.inProgress.length > 0
          ? 'IN PROGRESS'
          : bands.complete.length === programs.length
            ? 'COMPLETE'
            : 'IDLE'

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsVerdictStrip
        ariaLabel="Delivery board verdict"
        title="DELIVERY VERDICT"
        lamp={verdictLamp}
        tagLabel={verdictLabel}
        tagVariant={verdictTag}
        summary={
          isLoading
            ? 'Loading delivery programs…'
            : isError
              ? 'Failed to load delivery programs from API.'
              : programs.length === 0
                ? 'No programs match this Scope → Lane filter.'
                : `${programs.length} program${programs.length === 1 ? '' : 's'} · ${bands.inProgress.length} in progress · ${bands.complete.length} complete · ${bands.notStarted.length} not started`
        }
        actions={
          <div className="flex flex-wrap gap-2">
            {onOpenActiveSession != null && (
              <Button
                size="sm"
                variant="outline"
                className="shrink-0"
                onClick={() =>
                  onOpenActiveSession(
                    laneFilter != null ? { laneId: laneFilter } : undefined,
                  )
                }
              >
                Open Active Session
              </Button>
            )}
            {onOpenBriefing != null && (
              <Button
                size="sm"
                variant="outline"
                className="shrink-0"
                onClick={() => onOpenBriefing()}
              >
                Open Agent Briefing
              </Button>
            )}
          </div>
        }
        meta={
          <span>
            Read-only archive — program sign-off and post-completion Approve live in Active Session.
          </span>
        }
      />

      <DeliveryBoardFilterChrome
        scope={scope}
        trackType={trackType}
        laneId={laneFilter}
        onScopeChange={handleScopeChange}
        onTrackTypeChange={handleTrackTypeChange}
        onLaneChange={handleLaneChange}
        lanesWithPrograms={lanesWithPrograms}
        scopeBandCounts={scopeBandCounts}
      />

      {programsQuery.isLoading && (
        <p className="text-dense-meta text-muted-foreground">Loading programs…</p>
      )}
      {programsQuery.isError && (
        <p className="text-dense-meta text-destructive">Failed to load delivery programs from API.</p>
      )}

      {!programsQuery.isLoading && !programsQuery.isError && programs.length === 0 && (
        <p className="m-0 text-dense-meta text-muted-foreground">
          No programs match this Scope → Lane filter.
        </p>
      )}

      {bands.inProgress.length > 0 && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <BriefingStatusLamp status="doing" />
            <span className="text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
              In progress
            </span>
            <BriefingStatusBadge status="doing" label={`${bands.inProgress.length}`} />
          </div>
          <ProgramBandTable
            programs={bands.inProgress}
            selectedProgramId={selectedProgramId}
            onSelect={toggleSelect}
          />
        </div>
      )}

      {bands.notStarted.length > 0 && (
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-2">
            <BriefingStatusLamp status="ready" />
            <span className="text-dense-meta font-semibold uppercase tracking-wide text-muted-foreground">
              Not started
            </span>
            <BriefingStatusBadge status="ready" label={`${bands.notStarted.length}`} />
          </div>
          <ProgramBandTable
            programs={bands.notStarted}
            selectedProgramId={selectedProgramId}
            onSelect={toggleSelect}
          />
        </div>
      )}

      <DeliveryBoardCompleteGrid
        programs={bands.complete}
        selectedProgramId={selectedProgramId}
        onSelect={toggleSelect}
      />

      {selectedProgramId != null && selectedProgram != null && (
        <OpsSection
          title={selectedProgram.label}
          description={`${selectedProgram.signed}/${selectedProgram.gateCount} gates · ${selectedProgram.phasesDone}/${selectedProgram.phaseCount} done · read-only on Delivery Board · sign-off in Active Session`}
          overflow="visible"
        >
          <DeliveryBoardProgramPanels programId={selectedProgramId} allowSignOff={false} />
        </OpsSection>
      )}

      <DeliveryBoardHistoricalArchive />
    </div>
  )
}
