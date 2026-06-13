import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { AgentFocusDock } from '@/components/control-room/AgentFocusDock'
import { BayDetailDrawer } from '@/components/control-room/BayDetailDrawer'
import {
  DualFlywheelPanel,
  type ControlRoomSelection,
} from '@/components/control-room/DualFlywheelPanel'
import { PipelineFlow } from '@/components/control-room/PipelineFlow'
import { useState } from 'react'

interface ControlRoomPageProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  onOpenMatrix: () => void
  onOpenProgram: () => void
}

export function ControlRoomPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  onOpenMatrix,
  onOpenProgram,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)

  if (contextLoading || matrixLoading) {
    return <p className="text-[var(--muted-foreground)]">Loading control room…</p>
  }

  return (
    <div className="control-room-layout flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <h2 className="m-0 text-sm font-semibold">Control Room — dual flywheel governance (L0)</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Release pipeline, bay lamps, and Agent context packs. Read-only probes — no write actions.
        </p>
      </section>

      <DualFlywheelPanel
        context={context}
        matrices={matrices}
        selection={selection}
        onSelectBay={id => setSelection({ kind: 'bay', id })}
        onOpenProgram={onOpenProgram}
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
        onOpenMatrix={onOpenMatrix}
        onOpenProgram={onOpenProgram}
      />
    </div>
  )
}
