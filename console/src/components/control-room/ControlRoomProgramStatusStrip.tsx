import { DenseTag } from '@bifrost/ui'
import {
  CONTROL_ROOM_PROGRAM_PHASES,
  controlRoomProgramSignedCount,
  isControlRoomPhaseSignedOff,
} from '@/lib/control-room/controlRoomProgramStatus'
import { isControlRoomProgramComplete } from '@/lib/control-room/controlRoomPhase5Delivery'
import { isMissionSignalPhase7SignedOff } from '@/lib/control-room/missionSignalPhase7Delivery'
import { useControlRoomSignoffRevision } from '@/lib/control-room/controlRoomSignoffEvents'

export function ControlRoomProgramStatusStrip() {
  useControlRoomSignoffRevision()
  const { signed, total } = controlRoomProgramSignedCount()
  const allDone = signed === total
  const programClosed = isControlRoomProgramComplete()
  const missionSignalClosed = isMissionSignalPhase7SignedOff()

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Control Room commander
      </span>
      {CONTROL_ROOM_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isControlRoomPhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isControlRoomPhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      {programClosed ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          CONTROL ROOM COMMANDER COMPLETE
        </span>
      ) : allDone ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          All phases signed — commander program complete
        </span>
      ) : (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {signed}/{total} signed — panels below
        </span>
      )}
      {missionSignalClosed && !programClosed && (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          · Mission Signal program closed
        </span>
      )}
    </div>
  )
}
