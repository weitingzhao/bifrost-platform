import { DenseTag } from '@bifrost/ui'
import {
  UNIFI_MCP_SERVER_PROGRAM_PHASES,
  isUnifiMcpServerPhaseSignedOff,
  unifiMcpServerProgramSignedCount,
} from '@/lib/architecture/unifiMcpServerProgramStatus'
import { useGovernanceSignoffRevision } from '@/lib/architecture/governanceSignoffEvents'

export function UnifiMcpServerProgramStatusStrip() {
  useGovernanceSignoffRevision()
  const { signed, total } = unifiMcpServerProgramSignedCount()
  const allDone = signed === total

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        UniFi MCP Server stream
      </span>
      {UNIFI_MCP_SERVER_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isUnifiMcpServerPhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isUnifiMcpServerPhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      {allDone ? (
        <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
          UNIFI MCP SERVER STREAM COMPLETE
        </span>
      ) : (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {signed}/{total} signed — implementation track (post Network Governance)
        </span>
      )}
    </div>
  )
}
