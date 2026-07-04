/** Trade IB Client Migration — Phase 3 delivery (Celery bars RPC-only). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationPhase2SignedOff } from './tradeIbClientMigrationPhase2Delivery'

export const TRADE_IB_CLIENT_MIGRATION_PHASE3_VERSION = '2026-07-04'

export interface TradeIbClientMigrationPhase3DeliveryItem {
  id: 'TIBM3-1' | 'TIBM3-2' | 'TIBM3-3' | 'TIBM3-4' | 'TIBM3-5' | 'TIBM3-6' | 'TIBM3-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_PHASE3_DELIVERY_ITEMS: TradeIbClientMigrationPhase3DeliveryItem[] =
  [
    {
      id: 'TIBM3-1',
      title: 'IbOperatorBarsAdapter default transport',
      summary:
        'Celery bars worker uses Platform Gateway fetch_bars_range RPC — no MarketIbClient / direct TWS import.',
      verifySteps: [
        'tasks.py — _get_or_create_bars_ib_client returns IbOperatorBarsAdapter only.',
        'Removed bifrost_core.monitor.integrations.ib_clients import (module absent from core).',
        'pytest tests/test_ib_operator_bars_transport.py — all pass.',
      ],
    },
    {
      id: 'TIBM3-2',
      title: 'use_for_celery_bars default true',
      summary:
        'bifrost_core.ib_operator.config effective_ib_operator_settings defaults use_for_celery_bars when key absent.',
      verifySteps: [
        'pyproject.toml version 0.2.10.',
        'pytest tests/test_ib_operator_config.py — default true, explicit false honored.',
        'config.yaml.example + infra overlays set use_for_celery_bars: true.',
      ],
    },
    {
      id: 'TIBM3-3',
      title: 'backfill.py transport typing',
      summary: 'Shared backfill logic typed against IbOperatorBarsAdapter — no stale MarketIbClient refs.',
      verifySteps: [
        'backfill.py run_one_backfill ib_client param uses IbOperatorBarsAdapter TYPE_CHECKING import.',
        'fetch_bars_range duck-type unchanged — adapter implements ensure_connected + fetch_bars_range.',
      ],
    },
    {
      id: 'TIBM3-4',
      title: 'Cluster Celery bars verify script',
      summary: 'make verify-trade-celery-bars — ping + fetch_bars_range via trade-prod ACL on redis-ib.',
      verifySteps: [
        'scripts/verify-trade-celery-bars.sh — operator ping + fetch_bars_range SPY.',
        'Run after ib-gateway pod ready (mock or live).',
        'Worker image rebuild required for runtime switch from direct TWS.',
      ],
    },
    {
      id: 'TIBM3-5',
      title: 'Catalog surface S06 updated',
      summary: 'Inventory reflects Celery bars on Platform Gateway bus — direct_tws cleared.',
      verifySteps: [
        'S06 bars backfill → on_bus with IbOperatorBarsAdapter notes.',
        'Program overview TIBM3 in_progress until Owner sign-off.',
        'TIBM2 marked done after Phase 2 sign-off.',
      ],
    },
    {
      id: 'TIBM3-6',
      title: 'No worker direct TWS socket',
      summary: 'stocks_ib queue worker never opens ib_insync socket — only Redis operator RPC.',
      verifySteps: [
        'grep tasks.py — no MarketIbClient / ib_client_id_worker_market connect path.',
        'Worker IB status Redis key reflects operator ping (client_id from gateway host block).',
        'Explicit ib_operator.enabled=false fails fast with TIBM3 error message.',
      ],
    },
    {
      id: 'TIBM3-7',
      title: 'Phase 3 Owner sign-off',
      summary: 'Celery bars RPC-only verified — proceed to TIBM4 UI/legacy cleanup.',
      verifySteps: [
        'Mark TIBM3-1 … TIBM3-6 verified.',
        'Admin token — Sign off Phase 3 delivery.',
        'Program strip shows TIBM3 ✓; unlock TIBM4.',
      ],
    },
  ]

export interface TradeIbClientMigrationPhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationPhase3SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationPhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_phase3_signoff'

function emptyItemState(): TradeIbClientMigrationPhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationPhase3SignoffState(): TradeIbClientMigrationPhase3SignoffState {
  const items: Record<string, TradeIbClientMigrationPhase3ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationPhase3SignoffState(): TradeIbClientMigrationPhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationPhase3SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationPhase3SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_PHASE3_VERSION) {
      return defaultTradeIbClientMigrationPhase3SignoffState()
    }
    const merged = defaultTradeIbClientMigrationPhase3SignoffState()
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
    return defaultTradeIbClientMigrationPhase3SignoffState()
  }
}

export function saveTradeIbClientMigrationPhase3SignoffState(
  state: TradeIbClientMigrationPhase3SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationPhase3ItemsVerified(
  state: TradeIbClientMigrationPhase3SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_PHASE3_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationPhase3VerificationCount(
  state: TradeIbClientMigrationPhase3SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_PHASE3_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationPhase3SignedOff(): boolean {
  return loadTradeIbClientMigrationPhase3SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationPhase3Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationPhase2SignedOff()) {
    missing.push('Trade IB Migration Phase 2 sign-off (TIBM2) — required before TIBM3')
  }
  return { ok: missing.length === 0, missing }
}
