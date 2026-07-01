import { DenseTag } from '@bifrost/ui'
import {
  GOVERNANCE_PROGRAM_PHASES,
  governanceProgramSignedCount,
  isGovernancePhaseSignedOff,
} from '@/lib/architecture/governanceProgramStatus'
import { isGovernancePhase7SignedOff } from '@/lib/architecture/governancePhase7Delivery'
import { useGovernanceSignoffRevision } from '@/lib/architecture/governanceSignoffEvents'

export function GovernanceProgramStatusStrip() {
  useGovernanceSignoffRevision()
  const { signed, total } = governanceProgramSignedCount()
  const allDone = signed === total
  const programClosed = isGovernancePhase7SignedOff()

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Governance program
      </span>
      {GOVERNANCE_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isGovernancePhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isGovernancePhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      {programClosed ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          GOVERNANCE PROGRAM COMPLETE
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
