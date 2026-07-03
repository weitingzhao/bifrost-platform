import { DenseTag } from '@bifrost/ui'
import {
  NETWORK_GOVERNANCE_PROGRAM_PHASES,
  isNetworkGovernancePhaseSignedOff,
  networkGovernanceProgramSignedCount,
} from '@/lib/architecture/networkGovernanceProgramStatus'
import { isNetworkGovernancePhase8SignedOff } from '@/lib/architecture/networkGovernancePhase8Delivery'
import { useGovernanceSignoffRevision } from '@/lib/architecture/governanceSignoffEvents'

export function NetworkGovernanceProgramStatusStrip() {
  useGovernanceSignoffRevision()
  const { signed, total } = networkGovernanceProgramSignedCount()
  const allDone = signed === total
  const programClosed = isNetworkGovernancePhase8SignedOff()

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Network Governance program
      </span>
      {NETWORK_GOVERNANCE_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isNetworkGovernancePhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isNetworkGovernancePhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      {programClosed ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          NETWORK GOVERNANCE PROGRAM COMPLETE
        </span>
      ) : allDone ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          All phases signed — ready for Phase 8 program closure
        </span>
      ) : (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {signed}/{total} signed — complete panels below before Phase 8
        </span>
      )}
    </div>
  )
}
