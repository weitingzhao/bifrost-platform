/** Trade IB Client Migration — Phase 1 delivery (Gateway RPC parity). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationPhase0SignedOff } from './tradeIbClientMigrationPhase0Delivery'

export const TRADE_IB_CLIENT_MIGRATION_PHASE1_VERSION = '2026-07-04'

export interface TradeIbClientMigrationPhase1DeliveryItem {
  id: 'TIBM1-1' | 'TIBM1-2' | 'TIBM1-3' | 'TIBM1-4' | 'TIBM1-5' | 'TIBM1-6' | 'TIBM1-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_PHASE1_DELIVERY_ITEMS: TradeIbClientMigrationPhase1DeliveryItem[] =
  [
    {
      id: 'TIBM1-1',
      title: 'Live Gateway — all ALL_OPS',
      summary:
        'bifrost-platform-plugin live.py + ib_ops.py implement ping, disconnect/reconnect, fetch_bars_range, fetch_executions, option ops.',
      verifySteps: [
        'src/bifrost_plugin/ib_gateway/ib_ops.py — bars range, executions, option expirations/snapshot.',
        'live.py handle_command dispatches all 9 ops (no unsupported_op for ALL_OPS).',
        'Host slot used for market/data ops; account_slot payload for account ops.',
      ],
    },
    {
      id: 'TIBM1-2',
      title: 'Mock Gateway — all ALL_OPS',
      summary: 'MockGateway returns synthetic ok payloads for every op — CI + dev without TWS.',
      verifySteps: [
        'mock.py handle_command supports 9/9 ops.',
        'pytest tests/test_operator_ops.py — parametrized ALL_OPS pass.',
      ],
    },
    {
      id: 'TIBM1-3',
      title: 'Unit tests green',
      summary: 'tests/test_operator_ops.py + existing protocol/redis contract tests.',
      verifySteps: [
        'cd bifrost-platform-plugin && make test — all pass.',
        'No mock_unsupported_op / unsupported_op for catalog ALL_OPS list.',
      ],
    },
    {
      id: 'TIBM1-4',
      title: 'Cluster RPC parity script',
      summary: 'scripts/verify-ib-gateway-rpc-parity.sh — XADD each op via trade-prod ACL on redis-ib.',
      verifySteps: [
        'Rebuild + rollout ib-gateway image after code change.',
        'make verify-ib-gateway-rpc-parity — 9/9 ops return ok:true.',
        'Live mode: fetch_bars_range / option ops require TWS connected (may return empty data but ok:true).',
      ],
    },
    {
      id: 'TIBM1-5',
      title: 'Catalog RPC matrix updated',
      summary: 'tradeIbClientMigrationCatalog.ts RPC matrix — Platform Gateway column all yes for 9 ops.',
      verifySteps: [
        'TRADE_IB_RPC_OP_MATRIX platformGateway=yes for every row.',
        'S04 Operator RPC surface status → on_bus (client + gateway parity).',
        'Phase strip TIBM1 in_progress until Owner sign-off.',
      ],
    },
    {
      id: 'TIBM1-6',
      title: 'Trade callers unblocked',
      summary:
        'Monitor fetch_accounts_snapshot, Celery IbOperatorBarsAdapter (TIBM3), Research option_discovery can reach Gateway.',
      verifySteps: [
        'Monitor API daemon router — fetch_accounts_snapshot via IbOperatorClient works.',
        'Research option_discovery ops no longer hit unsupported_op on Gateway.',
        'Celery bars still defaults direct TWS until TIBM3 — but RPC path exists.',
      ],
    },
    {
      id: 'TIBM1-7',
      title: 'Phase 1 Owner sign-off',
      summary: 'Gateway RPC parity verified — proceed to TIBM2 health derivation.',
      verifySteps: [
        'Mark TIBM1-1 … TIBM1-6 verified.',
        'Admin token — Sign off Phase 1 delivery.',
        'Program strip shows TIBM1 ✓; unlock TIBM2.',
      ],
    },
  ]

export interface TradeIbClientMigrationPhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationPhase1SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationPhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_phase1_signoff'

function emptyItemState(): TradeIbClientMigrationPhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationPhase1SignoffState(): TradeIbClientMigrationPhase1SignoffState {
  const items: Record<string, TradeIbClientMigrationPhase1ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationPhase1SignoffState(): TradeIbClientMigrationPhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationPhase1SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationPhase1SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_PHASE1_VERSION) {
      return defaultTradeIbClientMigrationPhase1SignoffState()
    }
    const merged = defaultTradeIbClientMigrationPhase1SignoffState()
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
    return defaultTradeIbClientMigrationPhase1SignoffState()
  }
}

export function saveTradeIbClientMigrationPhase1SignoffState(
  state: TradeIbClientMigrationPhase1SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationPhase1ItemsVerified(
  state: TradeIbClientMigrationPhase1SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_PHASE1_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationPhase1VerificationCount(
  state: TradeIbClientMigrationPhase1SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_PHASE1_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationPhase1SignedOff(): boolean {
  return loadTradeIbClientMigrationPhase1SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationPhase1Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationPhase0SignedOff()) {
    missing.push('Trade IB Migration Phase 0 sign-off (TIBM0) — required before TIBM1')
  }
  return { ok: missing.length === 0, missing }
}
