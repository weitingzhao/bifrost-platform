/** Trade IB Client Migration — Phase 0 delivery (inventory & scope lock). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isIbGatewayPluginProgramSignedOff } from './ibGatewayPluginProgramDelivery'

export const TRADE_IB_CLIENT_MIGRATION_PHASE0_VERSION = '2026-07-04'

export interface TradeIbClientMigrationPhase0DeliveryItem {
  id: 'TIBM0-1' | 'TIBM0-2' | 'TIBM0-3' | 'TIBM0-4' | 'TIBM0-5' | 'TIBM0-6' | 'TIBM0-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_PHASE0_DELIVERY_ITEMS: TradeIbClientMigrationPhase0DeliveryItem[] =
  [
    {
      id: 'TIBM0-1',
      title: 'IB surface inventory table',
      summary:
        'All Trade IB touchpoints catalogued: quotes, account, RPC, Celery bars, daemon health, ops, FE — with repo paths and bus status.',
      verifySteps: [
        'tradeIbClientMigrationCatalog.ts — Surface inventory shows 14 rows (S01–S14).',
        'Each row has status tag: on_bus / partial / direct_tws / stale_ref / retired.',
        'Owner confirms no missing production IB path (daemon, api, worker, fe).',
      ],
    },
    {
      id: 'TIBM0-2',
      title: 'Operator RPC parity matrix',
      summary:
        'ALL_OPS from bifrost_core.ib_operator.protocol vs legacy socket executor vs Platform Gateway live/mock.',
      verifySteps: [
        'RPC matrix table lists 9 ops with legacy vs gateway coverage.',
        'Gap list explicit: fetch_bars_range, fetch_executions, option ops, disconnect/reconnect.',
        'Phase 1 scope = close all gaps used by Trade callers.',
      ],
    },
    {
      id: 'TIBM0-3',
      title: 'Read-path vs write-path classification',
      summary:
        'Reads (ticks, account hashes) already on redis-ib; writes (operator RPC, future orders) need Gateway parity.',
      verifySteps: [
        'S01–S03, S14 marked on_bus — no Phase 1 code change for pure reads.',
        'S04–S06, S08 flagged for RPC / transport work in Phase 1–3.',
        'Owner agrees: no second TWS connection from Trade K8s pods after migration.',
      ],
    },
    {
      id: 'TIBM0-4',
      title: 'Stale references documented',
      summary:
        'daemon_ib_edge trio health, Monitor socket blocks, ops market_ingest, FE socket ingest UI — all mapped to TIBM2/TIBM4.',
      verifySteps: [
        'S07, S09, S10, S11 status = stale_ref with target phase noted.',
        'Monitor status message already hints Platform gateway when legacy absent — FE still shows ib_ingestor labels.',
        'Owner accepts UI/API cleanup deferred to Phase 4 after RPC parity.',
      ],
    },
    {
      id: 'TIBM0-5',
      title: 'Phase 1–4 roadmap locked',
      summary:
        'TIBM1 Gateway ops · TIBM2 health derivation · TIBM3 Celery RPC-only · TIBM4 UI/legacy cleanup.',
      verifySteps: [
        'Program strip shows TIBM0 in_progress, TIBM1–4 pending.',
        'Dependencies: TIBM1 before TIBM3 (bars need fetch_bars_range); TIBM2 parallel with TIBM1.',
        'Copy Prompt exports full LLM pack for Agent implementation tasks.',
      ],
    },
    {
      id: 'TIBM0-6',
      title: 'IB Gateway Plugin prerequisite',
      summary: 'Migration assumes IBGP program complete — redis-ib live, Trade ExternalName, legacy socket retired.',
      verifySteps: [
        'Delivery Board → IB Gateway Plugin — program strip shows IBGP0–4 signed (or Owner acknowledges live gateway).',
        'make verify-ib-gateway-program passes on cluster (optional live check).',
        'If IBGP incomplete, block TIBM1+ until Platform bus stable.',
      ],
    },
    {
      id: 'TIBM0-7',
      title: 'Phase 0 Owner sign-off',
      summary: 'Inventory accurate; scope and phase order approved — proceed to TIBM1 Gateway RPC parity.',
      verifySteps: [
        'Mark TIBM0-1 … TIBM0-6 verified.',
        'Admin token — Sign off Phase 0 delivery.',
        'Program strip shows TIBM0 ✓; unlock Phase 1 work items.',
      ],
    },
  ]

export interface TradeIbClientMigrationPhase0ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationPhase0SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationPhase0ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_phase0_signoff'

function emptyItemState(): TradeIbClientMigrationPhase0ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationPhase0SignoffState(): TradeIbClientMigrationPhase0SignoffState {
  const items: Record<string, TradeIbClientMigrationPhase0ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_PHASE0_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_PHASE0_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationPhase0SignoffState(): TradeIbClientMigrationPhase0SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationPhase0SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationPhase0SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_PHASE0_VERSION) {
      return defaultTradeIbClientMigrationPhase0SignoffState()
    }
    const merged = defaultTradeIbClientMigrationPhase0SignoffState()
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
    return defaultTradeIbClientMigrationPhase0SignoffState()
  }
}

export function saveTradeIbClientMigrationPhase0SignoffState(
  state: TradeIbClientMigrationPhase0SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationPhase0ItemsVerified(
  state: TradeIbClientMigrationPhase0SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_PHASE0_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationPhase0VerificationCount(
  state: TradeIbClientMigrationPhase0SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_PHASE0_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_PHASE0_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationPhase0SignedOff(): boolean {
  return loadTradeIbClientMigrationPhase0SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationPhase0Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isIbGatewayPluginProgramSignedOff()) {
    missing.push('IB Gateway Plugin program sign-off (IBGP0–4) — recommended before TIBM1')
  }
  return { ok: missing.length === 0, missing }
}
