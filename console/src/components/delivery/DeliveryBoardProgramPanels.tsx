import { ProgramDetailView } from '@/components/delivery/ProgramDetailView'
import { MissionSignalProgramPanels } from '@/components/delivery/MissionSignalProgramPanels'
import { VisionProgramGatePanels } from '@/components/delivery/VisionProgramGatePanels'
export function DeliveryBoardProgramPanels({
  programId,
}: {
  programId: string
  signOffMechanism?: string
  matrices?: unknown[]
}) {
  return (
    <div className="flex flex-col gap-4">
      <ProgramDetailView programId={programId} />
      {programId === 'vision' && <VisionProgramGatePanels />}
      {programId === 'mission-signal' && <MissionSignalProgramPanels />}
    </div>
  )
}
