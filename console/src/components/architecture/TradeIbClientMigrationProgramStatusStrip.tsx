import { DenseTag } from '@bifrost/ui'
import {
  TRADE_IB_CLIENT_MIGRATION_PROGRAM_PHASES,
  isTradeIbClientMigrationPhaseSignedOff,
  tradeIbClientMigrationProgramSignedCount,
} from '@/lib/architecture/tradeIbClientMigrationProgramStatus'
import { useGovernanceSignoffRevision } from '@/lib/architecture/governanceSignoffEvents'
import { isTradeIbClientMigrationProgramSignedOff } from '@/lib/architecture/tradeIbClientMigrationProgramDelivery'

export function TradeIbClientMigrationProgramStatusStrip() {
  useGovernanceSignoffRevision()
  const { signed, total } = tradeIbClientMigrationProgramSignedCount()
  const programClosed = isTradeIbClientMigrationProgramSignedOff()

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--border)] bg-[var(--background)] px-3 py-2">
      <span className="text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        Trade IB Client migration
      </span>
      {programClosed && (
        <DenseTag variant="success" title="Program completion signed off">
          COMPLETE
        </DenseTag>
      )}
      {TRADE_IB_CLIENT_MIGRATION_PROGRAM_PHASES.map(p => (
        <DenseTag
          key={p.id}
          variant={isTradeIbClientMigrationPhaseSignedOff(p.id) ? 'success' : 'neutral'}
          title={p.signoffLocation}
        >
          {p.id}
          {isTradeIbClientMigrationPhaseSignedOff(p.id) ? ' ✓' : ''}
        </DenseTag>
      ))}
      <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
        {programClosed
          ? 'All phases signed — Trade IB Client Migration program complete'
          : `${signed}/${total} signed — Trade stack → Platform redis-ib bus (prerequisite: IBGP)`}
      </span>
    </div>
  )
}
