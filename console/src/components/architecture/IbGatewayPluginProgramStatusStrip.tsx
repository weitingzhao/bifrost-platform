import { DenseTag } from '@bifrost/ui'
import {
  IB_GATEWAY_PLUGIN_PROGRAM_PHASES,
  isIbGatewayPluginPhaseSignedOff,
  ibGatewayPluginProgramSignedCount,
} from '@/lib/architecture/ibGatewayPluginProgramStatus'
import { isIbGatewayPluginHardeningSignedOff } from '@/lib/architecture/ibGatewayPluginHardeningDelivery'
import { isIbGatewayPluginProgramSignedOff } from '@/lib/architecture/ibGatewayPluginProgramDelivery'
import { useGovernanceSignoffRevision } from '@/lib/architecture/governanceSignoffEvents'

export function IbGatewayPluginProgramStatusStrip() {
  useGovernanceSignoffRevision()
  const { signed, total } = ibGatewayPluginProgramSignedCount()
  const allPhasesDone = signed === total
  const programSigned = isIbGatewayPluginProgramSignedOff()
  const hardened = isIbGatewayPluginHardeningSignedOff()

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        IB Gateway Plugin program
      </span>
      {IB_GATEWAY_PLUGIN_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isIbGatewayPluginPhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isIbGatewayPluginPhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      {allPhasesDone ? (
        hardened ? (
          <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
            IB GATEWAY PLUGIN TERMINALIZED — ready for next program
          </span>
        ) : programSigned ? (
          <span className="text-[var(--text-dense-caption)] text-[var(--warning)]">
            Program signed — complete terminalization panel below
          </span>
        ) : (
          <span className="text-[var(--text-dense-caption)] text-[var(--success)]">
            IB GATEWAY PLUGIN COMPLETE — sign program panel below if not yet done
          </span>
        )
      ) : (
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {signed}/{total} signed — Platform Plugin (direct replacement, no parallel window)
        </span>
      )}
    </div>
  )
}
