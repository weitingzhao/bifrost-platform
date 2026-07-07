import {
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
import { DeliveryBoardHistoricalArchive } from '@/components/delivery/DeliveryBoardHistoricalArchive'
import { DeliveryBoardProgramPanels } from '@/components/delivery/DeliveryBoardProgramPanels'
import { PostCompletionPendingPanel } from '@/components/delivery/PostCompletionPendingPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import type { DeliveryBoardProgramId } from '@/lib/delivery/deliveryBoardPrograms'

function programStatusVariant(signed: number, complete: boolean): DenseTagVariant {
  if (complete) return 'success'
  if (signed > 0) return 'warning'
  return 'neutral'
}

export function DeliveryBoardPage() {
  const [selectedProgramId, setSelectedProgramId] = useState<DeliveryBoardProgramId | null>(null)

  const programsQuery = useQuery({
    queryKey: PROGRAMS_BOARD_QUERY_KEY,
    queryFn: fetchDeliveryBoardPrograms,
    staleTime: 15_000,
    refetchInterval: 30_000,
  })

  const programs = useMemo(
    () => (programsQuery.data?.programs ?? []).map(mapProgramSummaryToOverview),
    [programsQuery.data],
  )

  const selectedProgram = programs.find(p => p.id === selectedProgramId)

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Program overview"
        description="Delivery programs from platform-api GET /api/v1/programs?board=1 — sign-off persisted on server."
        overflow="visible"
      />

      <PostCompletionPendingPanel />

      {programsQuery.isLoading && (
        <p className="text-dense-meta text-muted-foreground">Loading programs…</p>
      )}
      {programsQuery.isError && (
        <p className="text-dense-meta text-destructive">Failed to load delivery programs from API.</p>
      )}

      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Program</DenseTableHead>
            <DenseTableHead>Phases</DenseTableHead>
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
                onClick={() =>
                  setSelectedProgramId(prev => (prev === program.id ? null : program.id))
                }
              >
                <DenseTableCell>
                  <div className="flex flex-col gap-0.5">
                    <span className="font-medium">{program.label}</span>
                    <span className="text-dense-meta text-muted-foreground">{program.description}</span>
                  </div>
                </DenseTableCell>
                <DenseTableCell className="font-mono-tabular">{program.phaseCount}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular">
                  {program.signed}/{program.phaseCount}
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

      {selectedProgramId != null && selectedProgram != null && (
        <OpsSection
          title={selectedProgram.label}
          description={`${selectedProgram.signed}/${selectedProgram.phaseCount} phases signed · formerly ${selectedProgram.formerLocation}`}
          overflow="visible"
        >
          <DeliveryBoardProgramPanels programId={selectedProgramId} />
        </OpsSection>
      )}

      <DeliveryBoardHistoricalArchive />
    </div>
  )
}
