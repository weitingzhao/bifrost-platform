import { isTradeIbClientMigrationPhase0SignedOff } from './tradeIbClientMigrationPhase0Delivery'
import { isTradeIbClientMigrationPhase1SignedOff } from './tradeIbClientMigrationPhase1Delivery'
import { isTradeIbClientMigrationPhase2SignedOff } from './tradeIbClientMigrationPhase2Delivery'
import { isTradeIbClientMigrationPhase3SignedOff } from './tradeIbClientMigrationPhase3Delivery'
import { isTradeIbClientMigrationPhase4SignedOff } from './tradeIbClientMigrationPhase4Delivery'

export type TradeIbClientMigrationProgramPhaseId = 'TIBM0' | 'TIBM1' | 'TIBM2' | 'TIBM3' | 'TIBM4'

export type TradeIbClientMigrationProgramPhaseMeta = {
  id: TradeIbClientMigrationProgramPhaseId
  shortLabel: string
  signoffLocation: string
}

export const TRADE_IB_CLIENT_MIGRATION_PROGRAM_PHASES: TradeIbClientMigrationProgramPhaseMeta[] = [
  {
    id: 'TIBM0',
    shortLabel: 'Inventory',
    signoffLocation: 'Mission Control → Delivery Board · Engineer → Briefing',
  },
  {
    id: 'TIBM1',
    shortLabel: 'Gateway RPC',
    signoffLocation: 'Mission Control → Delivery Board · Engineer → Briefing',
  },
  {
    id: 'TIBM2',
    shortLabel: 'Health / read',
    signoffLocation: 'Mission Control → Delivery Board · Engineer → Briefing',
  },
  {
    id: 'TIBM3',
    shortLabel: 'Workers',
    signoffLocation: 'Mission Control → Delivery Board · Engineer → Briefing',
  },
  {
    id: 'TIBM4',
    shortLabel: 'UI cleanup',
    signoffLocation: 'Mission Control → Delivery Board · Engineer → Briefing',
  },
]

const SIGNED_OFF: Partial<Record<TradeIbClientMigrationProgramPhaseId, () => boolean>> = {
  TIBM0: isTradeIbClientMigrationPhase0SignedOff,
  TIBM1: isTradeIbClientMigrationPhase1SignedOff,
  TIBM2: isTradeIbClientMigrationPhase2SignedOff,
  TIBM3: isTradeIbClientMigrationPhase3SignedOff,
  TIBM4: isTradeIbClientMigrationPhase4SignedOff,
}

export function isTradeIbClientMigrationPhaseSignedOff(
  id: TradeIbClientMigrationProgramPhaseId,
): boolean {
  return SIGNED_OFF[id]?.() ?? false
}

export function tradeIbClientMigrationProgramSignedCount(): { signed: number; total: number } {
  const signed = TRADE_IB_CLIENT_MIGRATION_PROGRAM_PHASES.filter(p =>
    isTradeIbClientMigrationPhaseSignedOff(p.id),
  ).length
  return { signed, total: TRADE_IB_CLIENT_MIGRATION_PROGRAM_PHASES.length }
}
