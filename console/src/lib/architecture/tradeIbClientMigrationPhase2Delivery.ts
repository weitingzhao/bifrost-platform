/** Trade IB Client Migration — Phase 2 delivery (read-path + health derivation). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationPhase1SignedOff } from './tradeIbClientMigrationPhase1Delivery'

export const TRADE_IB_CLIENT_MIGRATION_PHASE2_VERSION = '2026-07-04'

export interface TradeIbClientMigrationPhase2DeliveryItem {
  id: 'TIBM2-1' | 'TIBM2-2' | 'TIBM2-3' | 'TIBM2-4' | 'TIBM2-5' | 'TIBM2-6' | 'TIBM2-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_PHASE2_DELIVERY_ITEMS: TradeIbClientMigrationPhase2DeliveryItem[] =
  [
    {
      id: 'TIBM2-1',
      title: 'platform_ib_gateway core module',
      summary:
        'bifrost_core.monitor.integrations.platform_ib_gateway — detect plugin=ib-gateway, rollup lamp, daemon heartbeat.',
      verifySteps: [
        'platform_ib_gateway.py — is_platform_ib_gateway_health, build_platform_ib_gateway_status.',
        'derive_daemon_ib_heartbeat_from_redis returns ib_transport + platform_ib_gateway block.',
        'pytest tests/test_platform_ib_gateway.py — all pass.',
      ],
    },
    {
      id: 'TIBM2-2',
      title: 'Daemon ib_connected derivation',
      summary:
        'Worker daemon heartbeat uses Platform gateway redis-ib health — not legacy socket STS names.',
      verifySteps: [
        'daemon_ib_edge.py re-exports platform-aware derive_daemon_ib_heartbeat_from_redis.',
        'control_heartbeat.py unchanged import path — picks up TIBM2 behavior.',
        'ib_connected true when ingestor + account + operator rollups green.',
      ],
    },
    {
      id: 'TIBM2-3',
      title: 'Monitor GET /status platform block',
      summary:
        'socket.platform_ib_gateway aggregate + transport tags on ib_* socket blocks.',
      verifySteps: [
        'monitor/routers/status.py — socket.platform_ib_gateway in schema v8.',
        'ib_ingestor / ib_account_agent / ib_operator include transport=platform_gateway when plugin present.',
        'Legacy socket blocks still returned for backward compat (TIBM4 FE relabel).',
      ],
    },
    {
      id: 'TIBM2-4',
      title: 'Cluster health verify script',
      summary: 'make verify-trade-ib-health — redis-ib health hashes plugin=ib-gateway + tick read.',
      verifySteps: [
        'scripts/verify-trade-ib-health.sh — three ws_ib_* hashes + NVDA tick.',
        'Run after ib-gateway pod writing health (mock or live).',
        'Monitor API platform_ib_gateway visible after Trade images pick up bifrost-core 0.2.9+.',
      ],
    },
    {
      id: 'TIBM2-5',
      title: 'Catalog surface S07/S09 updated',
      summary: 'Inventory reflects health derivation complete — stale_ref cleared for daemon + Monitor API.',
      verifySteps: [
        'S07 daemon_ib_edge → on_bus with platform gateway notes.',
        'S09 Monitor status API → on_bus; platform_ib_gateway documented.',
        'Program overview TIBM2 in_progress until Owner sign-off.',
      ],
    },
    {
      id: 'TIBM2-6',
      title: 'bifrost-core 0.2.9',
      summary: 'Patch release — new platform_ib_gateway module + daemon heartbeat fields.',
      verifySteps: [
        'pyproject.toml version 0.2.9.',
        'Trade worker/api images rebuild with BIFROST_CORE_REF or pip -e after merge.',
        'No breaking removal — additive ib_transport / platform_ib_gateway fields.',
      ],
    },
    {
      id: 'TIBM2-7',
      title: 'Phase 2 Owner sign-off',
      summary: 'Health derivation from Platform gateway verified — proceed to TIBM3 Celery RPC-only.',
      verifySteps: [
        'Mark TIBM2-1 … TIBM2-6 verified.',
        'Admin token — Sign off Phase 2 delivery.',
        'Program strip shows TIBM2 ✓; unlock TIBM3.',
      ],
    },
  ]

export interface TradeIbClientMigrationPhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationPhase2SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationPhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_phase2_signoff'

function emptyItemState(): TradeIbClientMigrationPhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationPhase2SignoffState(): TradeIbClientMigrationPhase2SignoffState {
  const items: Record<string, TradeIbClientMigrationPhase2ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationPhase2SignoffState(): TradeIbClientMigrationPhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationPhase2SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationPhase2SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_PHASE2_VERSION) {
      return defaultTradeIbClientMigrationPhase2SignoffState()
    }
    const merged = defaultTradeIbClientMigrationPhase2SignoffState()
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
    return defaultTradeIbClientMigrationPhase2SignoffState()
  }
}

export function saveTradeIbClientMigrationPhase2SignoffState(
  state: TradeIbClientMigrationPhase2SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationPhase2ItemsVerified(
  state: TradeIbClientMigrationPhase2SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_PHASE2_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationPhase2VerificationCount(
  state: TradeIbClientMigrationPhase2SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_PHASE2_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationPhase2SignedOff(): boolean {
  return loadTradeIbClientMigrationPhase2SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationPhase2Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationPhase1SignedOff()) {
    missing.push('Trade IB Migration Phase 1 sign-off (TIBM1) — required before TIBM2')
  }
  return { ok: missing.length === 0, missing }
}
