import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { AuditPageLink } from '@/components/AuditPageLink'
import { AgentFocusDock } from '@/components/control-room/AgentFocusDock'
import { BayDetailDrawer } from '@/components/control-room/BayDetailDrawer'
import {
  DualFlywheelPanel,
  type ControlRoomSelection,
} from '@/components/control-room/DualFlywheelPanel'
import { PipelineFlow } from '@/components/control-room/PipelineFlow'
import { OpsSection } from '@/components/layout/OpsSection'
import { useState } from 'react'

interface ControlRoomPageProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  onOpenRuntimeMap: () => void
  onOpenProgram: () => void
  onOpenDelivery: () => void
  onOpenAudit: () => void
}

export function ControlRoomPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  onOpenRuntimeMap,
  onOpenProgram,
  onOpenDelivery,
  onOpenAudit,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)

  if (contextLoading || matrixLoading) {
    return <p className="text-[var(--muted-foreground)]">Loading control room…</p>
  }

  return (
    <div className="control-room-layout flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Dual flywheel governance"
        description={
          <>
            Program milestone spine, bay lamps, and Agent context packs. CI/CD path diagram lives on{' '}
            <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
              Delivery
            </button>
            . Read-only probes — no write actions (L0).
          </>
        }
        headerExtra={<AuditPageLink onOpenAudit={onOpenAudit} className="mt-2" />}
        overflow="visible"
      />

      <DualFlywheelPanel
        context={context}
        matrices={matrices}
        selection={selection}
        onSelectBay={id => setSelection({ kind: 'bay', id })}
        onOpenProgram={onOpenProgram}
        onOpenDelivery={onOpenDelivery}
      />

      {context != null && (
        <PipelineFlow
          context={context}
          selectionId={selection?.kind === 'milestone' ? selection.id : null}
          onSelectMilestone={id => setSelection({ kind: 'milestone', id })}
        />
      )}

      <AgentFocusDock context={context} matrices={matrices} selection={selection} />

      <BayDetailDrawer
        selection={selection}
        context={context}
        matrices={matrices}
        onClose={() => setSelection(null)}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onOpenProgram={onOpenProgram}
      />
    </div>
  )
}
