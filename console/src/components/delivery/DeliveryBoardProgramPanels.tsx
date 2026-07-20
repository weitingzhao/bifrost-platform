import { ProgramDetailView } from '@/components/delivery/ProgramDetailView'
import { MissionSignalProgramPanels } from '@/components/delivery/MissionSignalProgramPanels'
import { VisionProgramGatePanels } from '@/components/delivery/VisionProgramGatePanels'

export function DeliveryBoardProgramPanels({
  programId,
  allowSignOff = true,
}: {
  programId: string
  /** false = Delivery Board catalog; true = Briefing Session write host */
  allowSignOff?: boolean
  signOffMechanism?: string
  matrices?: unknown[]
}) {
  return (
    <div className="flex flex-col gap-4">
      <ProgramDetailView programId={programId} allowSignOff={allowSignOff} />
      {allowSignOff && programId === 'vision' && <VisionProgramGatePanels />}
      {allowSignOff && programId === 'mission-signal' && <MissionSignalProgramPanels />}
    </div>
  )
}
