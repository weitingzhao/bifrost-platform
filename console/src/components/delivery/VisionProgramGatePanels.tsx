import { VisionGatePanel } from '@/components/architecture/VisionGatePanel'
import { VISION_GATE_IDS } from '@/api/vision'

export function VisionProgramGatePanels() {
  return (
    <section className="page-section panel-elevated px-2 py-2 flex flex-col gap-3">
      <p className="text-dense-label font-medium m-0 px-3 pt-2">Vision gates</p>
      <p className="text-dense-meta text-muted-foreground m-0 px-3">
        Run gate checks here in Briefing Session (lane governance). Delivery Board shows progress as a read-only catalog.
      </p>
      {VISION_GATE_IDS.map(id => (
        <div key={id} className="border-t border-border/50 pt-1 first:border-t-0">
          <VisionGatePanel id={id} />
        </div>
      ))}
    </section>
  )
}
