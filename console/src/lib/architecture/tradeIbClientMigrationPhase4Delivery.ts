/** Trade IB Client Migration — Phase 4 delivery (UI + legacy cleanup). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationPhase3SignedOff } from './tradeIbClientMigrationPhase3Delivery'

export const TRADE_IB_CLIENT_MIGRATION_PHASE4_VERSION = '2026-07-04'

export interface TradeIbClientMigrationPhase4DeliveryItem {
  id: 'TIBM4-1' | 'TIBM4-2' | 'TIBM4-3' | 'TIBM4-4' | 'TIBM4-5' | 'TIBM4-6' | 'TIBM4-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_PHASE4_DELIVERY_ITEMS: TradeIbClientMigrationPhase4DeliveryItem[] =
  [
    {
      id: 'TIBM4-1',
      title: 'FE platformIbGateway module',
      summary:
        'Trade Frontend reads socket.platform_ib_gateway + transport tags — Platform Gateway labels, not legacy STS.',
      verifySteps: [
        'utils/platformIbGateway.ts — isPlatformIbGatewayActive, platformIbGatewayAggregateLamp.',
        'types/monitor.ts — StatusPlatformIbGateway on StatusSocket.',
        'vitest platformIbGateway.test.ts — all pass.',
      ],
    },
    {
      id: 'TIBM4-2',
      title: 'socketIngestLamp + ibBroker relabel',
      summary:
        'IB category, nav lamps, health tooltips reference Platform IB Gateway @ redis-ib (data/ib-gateway).',
      verifySteps: [
        'INGEST_CATEGORY_LABELS IB → Platform IB Gateway (redis-ib).',
        'ibBrokerRedisHealthLamp titles use Platform scope when transport=platform_gateway.',
        'SocketPageHeader copy updated — no legacy trade-socket STS wording.',
      ],
    },
    {
      id: 'TIBM4-3',
      title: 'Ops market_ingest config labels',
      summary:
        'Default service rows label IB trio as Platform IB Gateway components; stale systemd names retained for compat.',
      verifySteps: [
        'market_ingest_config.py — Platform IB Gateway · Market ingest / Account / Operator RPC.',
        'pytest test_market_ingest_platform_gateway.py — labels + ingest_health_is_platform_gateway.',
      ],
    },
    {
      id: 'TIBM4-4',
      title: 'Ops API platform_gateway_managed',
      summary:
        'GET /ops/market-ingest/services flags platform_gateway_managed + transport when health plugin=ib-gateway.',
      verifySteps: [
        'market_ingest_health_clear.ingest_health_is_platform_gateway.',
        'market_ingest.py — platform_gateway_managed on IB rows; STG control reject cites data/ib-gateway.',
      ],
    },
    {
      id: 'TIBM4-5',
      title: 'Cluster UI verify script',
      summary: 'make verify-trade-ib-ui — health plugin tags + source grep for FE/ops relabel.',
      verifySteps: [
        'scripts/verify-trade-ib-ui.sh — three ws_ib_* plugin=ib-gateway + FE/ops source checks.',
        'Run on cluster with ib-gateway writing health (mock or live).',
      ],
    },
    {
      id: 'TIBM4-6',
      title: 'Catalog S10/S11 cleared',
      summary: 'Inventory stale_ref cleared — Ops market ingest + FE socket UI on Platform gateway bus.',
      verifySteps: [
        'S10 Ops market ingest → on_bus.',
        'S11 FE socket ingest UI → on_bus.',
        'Program overview TIBM4 in_progress until Owner sign-off; TIBM3 done.',
      ],
    },
    {
      id: 'TIBM4-7',
      title: 'Phase 4 Owner sign-off',
      summary: 'Trade IB Client Migration program complete — all surfaces on Platform redis-ib bus.',
      verifySteps: [
        'Mark TIBM4-1 … TIBM4-6 verified.',
        'Admin token — Sign off Phase 4 delivery.',
        'Program strip shows TIBM4 ✓; migration program complete.',
      ],
    },
  ]

export interface TradeIbClientMigrationPhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationPhase4SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationPhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_phase4_signoff'

function emptyItemState(): TradeIbClientMigrationPhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationPhase4SignoffState(): TradeIbClientMigrationPhase4SignoffState {
  const items: Record<string, TradeIbClientMigrationPhase4ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationPhase4SignoffState(): TradeIbClientMigrationPhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationPhase4SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationPhase4SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_PHASE4_VERSION) {
      return defaultTradeIbClientMigrationPhase4SignoffState()
    }
    const merged = defaultTradeIbClientMigrationPhase4SignoffState()
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
    return defaultTradeIbClientMigrationPhase4SignoffState()
  }
}

export function saveTradeIbClientMigrationPhase4SignoffState(
  state: TradeIbClientMigrationPhase4SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationPhase4ItemsVerified(
  state: TradeIbClientMigrationPhase4SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_PHASE4_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationPhase4VerificationCount(
  state: TradeIbClientMigrationPhase4SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_PHASE4_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationPhase4SignedOff(): boolean {
  return loadTradeIbClientMigrationPhase4SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationPhase4Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationPhase3SignedOff()) {
    missing.push('Trade IB Migration Phase 3 sign-off (TIBM3) — required before TIBM4')
  }
  return { ok: missing.length === 0, missing }
}
