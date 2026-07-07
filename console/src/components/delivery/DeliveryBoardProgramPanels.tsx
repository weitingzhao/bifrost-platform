import { ProgramDetailView } from '@/components/delivery/ProgramDetailView'
import { VisionProgramGatePanels } from '@/components/delivery/VisionProgramGatePanels'
import type { DeliveryBoardProgramId } from '@/lib/delivery/deliveryBoardPrograms'

export function DeliveryBoardProgramPanels({
  programId,
}: {
  programId: DeliveryBoardProgramId
  signOffMechanism?: string
  matrices?: unknown[]
}) {
  return (
    <div className="flex flex-col gap-4">
      <ProgramDetailView programId={programId} />
      {programId === 'vision' && <VisionProgramGatePanels />}
    </div>
  )
}
