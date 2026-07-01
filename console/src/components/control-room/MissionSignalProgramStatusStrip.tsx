import { DenseTag } from '@bifrost/ui'
import {
  MISSION_SIGNAL_PROGRAM_PHASES,
  isMissionSignalPhaseSignedOff,
  missionSignalProgramSignedCount,
} from '@/lib/control-room/missionSignalProgramStatus'
import { isMissionSignalPhase7SignedOff } from '@/lib/control-room/missionSignalPhase7Delivery'
import { useMissionSignalSignoffRevision } from '@/lib/control-room/missionSignalSignoffEvents'

export function MissionSignalProgramStatusStrip() {
  useMissionSignalSignoffRevision()
  const { signed, total } = missionSignalProgramSignedCount()
  const allDone = signed === total
  const programClosed = isMissionSignalPhase7SignedOff()

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Mission Signal program
      </span>
      {MISSION_SIGNAL_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isMissionSignalPhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isMissionSignalPhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      {programClosed ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          MISSION SIGNAL PROGRAM COMPLETE
        </span>
      ) : allDone ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          All phases signed — ready for Phase 7 program closure
        </span>
      ) : (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {signed}/{total} signed — complete panels below before Phase 7
        </span>
      )}
    </div>
  )
}
