import { VisionS3GatePanel } from '@/components/architecture/VisionS3GatePanel'
import { VisionV1GatePanel } from '@/components/architecture/VisionV1GatePanel'
import { VisionV2GatePanel } from '@/components/architecture/VisionV2GatePanel'
import { VisionV3GatePanel } from '@/components/architecture/VisionV3GatePanel'
import { VisionV4GatePanel } from '@/components/architecture/VisionV4GatePanel'
import { VisionV5GatePanel } from '@/components/architecture/VisionV5GatePanel'

const VISION_GATES = [
  { id: 'V5', Panel: VisionV5GatePanel },
  { id: 'V4', Panel: VisionV4GatePanel },
  { id: 'V3', Panel: VisionV3GatePanel },
  { id: 'V2', Panel: VisionV2GatePanel },
  { id: 'S3', Panel: VisionS3GatePanel },
  { id: 'V1', Panel: VisionV1GatePanel },
] as const

export function VisionProgramGatePanels() {
  return (
    <section className="page-section panel-elevated px-2 py-2 flex flex-col gap-3">
      <p className="text-dense-label font-medium m-0 px-3 pt-2">Vision gates</p>
      <p className="text-dense-meta text-muted-foreground m-0 px-3">
        Run gate checks and record Owner sign-off per gate. Phase table above syncs from unified programs API.
      </p>
      {VISION_GATES.map(({ id, Panel }) => (
        <div key={id} className="border-t border-border/50 pt-1 first:border-t-0">
          <Panel />
        </div>
      ))}
    </section>
  )
}
