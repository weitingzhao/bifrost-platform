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
import { fetchMatrix, isAllMatrices } from '@/api/platform'
import { DeliveryBoardHistoricalArchive } from '@/components/delivery/DeliveryBoardHistoricalArchive'
import { DeliveryBoardProgramPanels } from '@/components/delivery/DeliveryBoardProgramPanels'
import { OpsSection } from '@/components/layout/OpsSection'
import { useGovernanceSignoffRevision } from '@/lib/architecture/governanceSignoffEvents'
import { useBriefingSignoffRevision } from '@/lib/briefing/briefingSignoffEvents'
import { useControlRoomSignoffRevision } from '@/lib/control-room/controlRoomSignoffEvents'
import { useMissionSignalSignoffRevision } from '@/lib/control-room/missionSignalSignoffEvents'
import {
  buildDeliveryBoardProgramOverview,
  type DeliveryBoardProgramId,
} from '@/lib/delivery/deliveryBoardPrograms'

function programStatusVariant(signed: number, complete: boolean): DenseTagVariant {
  if (complete) return 'success'
  if (signed > 0) return 'warning'
  return 'neutral'
}

export function DeliveryBoardPage() {
  const crRev = useControlRoomSignoffRevision()
  const msRev = useMissionSignalSignoffRevision()
  const govRev = useGovernanceSignoffRevision()
  const briefingRev = useBriefingSignoffRevision()

  const [selectedProgramId, setSelectedProgramId] = useState<DeliveryBoardProgramId | null>(null)

  const matrixQuery = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    enabled: selectedProgramId === 'mission-signal',
  })

  const programs = useMemo(
    () => buildDeliveryBoardProgramOverview(),
    [crRev, msRev, govRev, briefingRev],
  )

  const selectedProgram = programs.find(p => p.id === selectedProgramId)

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Program overview"
        description="Console delivery programs — phased sign-off checklists moved out of functional pages. Select a program to expand its panels."
        overflow="visible"
      />

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
          <DeliveryBoardProgramPanels
            programId={selectedProgramId}
            matrices={
              matrixQuery.data != null && isAllMatrices(matrixQuery.data)
                ? matrixQuery.data.matrices
                : matrixQuery.data != null && !isAllMatrices(matrixQuery.data)
                  ? [matrixQuery.data]
                  : []
            }
          />
        </OpsSection>
      )}

      <DeliveryBoardHistoricalArchive />
    </div>
  )
}
