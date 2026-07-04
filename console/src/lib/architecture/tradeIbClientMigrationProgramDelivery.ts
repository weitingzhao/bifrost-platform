/** Trade IB Client Migration — Program completion (after TIBM0–4 phase sign-offs). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { tradeIbClientMigrationProgramSignedCount } from './tradeIbClientMigrationProgramStatus'

export const TRADE_IB_CLIENT_MIGRATION_PROGRAM_VERSION = '2026-07-04'

export interface TradeIbClientMigrationProgramDeliveryItem {
  id: 'TIBM-PC-1' | 'TIBM-PC-2' | 'TIBM-PC-3' | 'TIBM-PC-4' | 'TIBM-PC-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_PROGRAM_DELIVERY_ITEMS: TradeIbClientMigrationProgramDeliveryItem[] =
  [
    {
      id: 'TIBM-PC-1',
      title: 'verify-trade-ib-migration-program.sh',
      summary: 'Aggregate script — RPC parity + health + Celery bars + UI relabel gates.',
      verifySteps: [
        'make verify-trade-ib-migration-program in bifrost-platform-plugin.',
        'Runs verify-ib-gateway-rpc-parity + verify-trade-ib-health + verify-trade-celery-bars + verify-trade-ib-ui.',
        'All four steps must pass before program sign-off.',
      ],
    },
    {
      id: 'TIBM-PC-2',
      title: 'Phase sign-offs TIBM0–4',
      summary: 'All five phase panels signed — Trade stack consumes Platform redis-ib bus only.',
      verifySteps: [
        'Program strip shows TIBM0 ✓ … TIBM4 ✓.',
        'Each phase panel status SIGNED with timestamp.',
        'Phase 4 closed UI/legacy stale references (S10/S11 on_bus).',
      ],
    },
    {
      id: 'TIBM-PC-3',
      title: 'Platform IB bus end-to-end',
      summary: 'data/ib-gateway writes health @ redis-ib; Trade reads ticks + operator RPC; no worker direct TWS.',
      verifySteps: [
        'redis-ib health hashes plugin=ib-gateway (mock or live).',
        'fetch_bars_range RPC OK (Celery path).',
        'Trade images rollout: core 0.2.10+, worker/api/ops/fe deploy for runtime parity.',
      ],
    },
    {
      id: 'TIBM-PC-4',
      title: 'Catalog + inventory closed',
      summary: 'tradeIbClientMigrationCatalog — TIBM0–4 done; no stale_ref/direct_tws on migrated surfaces.',
      verifySteps: [
        'Program overview — all five implementation phases status done.',
        'Surface inventory: S01–S07,S09–S12 on_bus or retired (S13); S04/S08 partial documented.',
        'Prerequisite IBGP program signed (Platform owns TWS).',
      ],
    },
    {
      id: 'TIBM-PC-5',
      title: 'Program completion sign-off',
      summary: 'Owner confirms Trade IB Client Migration complete — Trade is bus-only on Platform redis-ib.',
      verifySteps: [
        'This panel visible after TIBM4 signed off.',
        'Mark all TIBM-PC items verified → Sign off program (Admin token).',
        'Strip shows TRADE IB MIGRATION COMPLETE.',
      ],
    },
  ]

export interface TradeIbClientMigrationProgramItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationProgramSignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationProgramItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_program_signoff'

function emptyItemState(): TradeIbClientMigrationProgramItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationProgramSignoffState(): TradeIbClientMigrationProgramSignoffState {
  const items: Record<string, TradeIbClientMigrationProgramItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_PROGRAM_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_PROGRAM_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationProgramSignoffState(): TradeIbClientMigrationProgramSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationProgramSignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationProgramSignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_PROGRAM_VERSION) {
      return defaultTradeIbClientMigrationProgramSignoffState()
    }
    const merged = defaultTradeIbClientMigrationProgramSignoffState()
    for (const id of Object.keys(merged.items)) {
      if (parsed.items[id] != null) {
        merged.items[id] = parsed.items[id]
      }
    }
    merged.signedOffAt = parsed.signedOffAt
    merged.signedOffBy = parsed.signedOffBy
    merged.note = parsed.note
    return merged
  } catch {
    return defaultTradeIbClientMigrationProgramSignoffState()
  }
}

export function saveTradeIbClientMigrationProgramSignoffState(
  state: TradeIbClientMigrationProgramSignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationProgramItemsVerified(
  state: TradeIbClientMigrationProgramSignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_PROGRAM_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationProgramVerificationCount(
  state: TradeIbClientMigrationProgramSignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_PROGRAM_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_PROGRAM_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationProgramSignedOff(): boolean {
  return loadTradeIbClientMigrationProgramSignoffState().signedOffAt != null
}

export function allTradeIbClientMigrationPhasesSignedOff(): boolean {
  const { signed, total } = tradeIbClientMigrationProgramSignedCount()
  return signed === total
}

export function priorTradeIbClientMigrationProgramPrerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!allTradeIbClientMigrationPhasesSignedOff()) {
    missing.push('All phase sign-offs TIBM0–TIBM4 required')
  }
  return { ok: missing.length === 0, missing }
}
