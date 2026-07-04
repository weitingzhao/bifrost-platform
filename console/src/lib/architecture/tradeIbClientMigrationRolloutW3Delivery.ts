/** TIBM Rollout W3 — STG read-only API domains. D10: no daemon / no order mutations. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationRolloutW2SignedOff } from './tradeIbClientMigrationRolloutW2Delivery'

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_VERSION = '2026-07-04'

export interface TradeIbClientMigrationRolloutW3DeliveryItem {
  id: 'TIBM-W3-1' | 'TIBM-W3-2' | 'TIBM-W3-3' | 'TIBM-W3-4' | 'TIBM-W3-5' | 'TIBM-W3-6' | 'TIBM-W3-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_DELIVERY_ITEMS: TradeIbClientMigrationRolloutW3DeliveryItem[] =
  [
    {
      id: 'TIBM-W3-1',
      title: 'STG W3 API images rebuilt',
      summary:
        'bifrost-api-market, massive, research, portfolio, docs, trading :stg with bifrost-core >= 0.2.10.',
      verifySteps: [
        'make rollout-tibm-w3-stg in bifrost-platform-plugin.',
        'Registry 192.168.10.73:30500 — six API images pushed (linux/amd64).',
        'api-strategy intentionally excluded from W3 scope.',
      ],
    },
    {
      id: 'TIBM-W3-2',
      title: 'Rollout restart W3 API deployments',
      summary: 'Six read-only API Deployments available; daemon stays replicas:0 (D10).',
      verifySteps: [
        'kubectl rollout status for api-market … api-trading in bifrost-stg.',
        'kubectl get deploy daemon — replicas 0.',
        'Do not exercise order POST paths as rollout gate.',
      ],
    },
    {
      id: 'TIBM-W3-3',
      title: 'bifrost-core >= 0.2.10 on W3 pods',
      summary: 'All W3 API pods aligned on Platform IB Gateway–capable core.',
      verifySteps: [
        'kubectl exec each W3 deploy — pip show bifrost-core Version >= 0.2.10.',
        'make verify-trade-ib-w3-stg step [3/6].',
      ],
    },
    {
      id: 'TIBM-W3-4',
      title: 'Ingress /health — all W3 domains',
      summary: 'Traefik smoke — GET /api/{domain}/health returns 200 for each rolled domain.',
      verifySteps: [
        'curl -H Host:trade-stg.bifrost.lan http://192.168.10.73/api/market/health (and massive, research, portfolio, docs, trading).',
        'make verify-trade-ib-w3-stg step [4/6].',
      ],
    },
    {
      id: 'TIBM-W3-5',
      title: 'Market quotes smoke',
      summary:
        'GET /api/market/quotes returns 200 with quotes[] key; api-market reaches redis_ib. Full tick E2E when Gateway writes ticks.',
      verifySteps: [
        'GET /api/market/quotes?symbols=NVDA — not "Redis unavailable" after rollout.',
        'api-market pod — redis_ib PING OK.',
        'Optional: make verify-trade-quotes-e2e when ib:ingester:tick:* present (live/mock tick writer).',
        'GET /api/trading/executions/freshness HTTP 200 (read-only smoke).',
      ],
    },
    {
      id: 'TIBM-W3-6',
      title: 'Trade FE read paths (optional)',
      summary: 'Live quotes / research pages load against rolled APIs — observe only, no live orders.',
      verifySteps: [
        'Trade STG → Market → Live — watchlist symbols show quotes when Gateway mock/live tick present.',
        'Research / Portfolio read pages return data without 5xx.',
      ],
    },
    {
      id: 'TIBM-W3-7',
      title: 'W3 Owner sign-off',
      summary: 'Confirm STG read-only API rollout complete — D10 still blocks live trading.',
      verifySteps: [
        'Mark TIBM-W3-1 … TIBM-W3-6 verified.',
        'Sign off W3 (Admin token).',
        'W-block (daemon / live execution) remains BLOCKED until Owner D10 unlock.',
      ],
    },
  ]

export interface TradeIbClientMigrationRolloutW3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationRolloutW3SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationRolloutW3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_rollout_w3_signoff'

function emptyItemState(): TradeIbClientMigrationRolloutW3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationRolloutW3SignoffState(): TradeIbClientMigrationRolloutW3SignoffState {
  const items: Record<string, TradeIbClientMigrationRolloutW3ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationRolloutW3SignoffState(): TradeIbClientMigrationRolloutW3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationRolloutW3SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationRolloutW3SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_VERSION) {
      return defaultTradeIbClientMigrationRolloutW3SignoffState()
    }
    const merged = defaultTradeIbClientMigrationRolloutW3SignoffState()
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
    return defaultTradeIbClientMigrationRolloutW3SignoffState()
  }
}

export function saveTradeIbClientMigrationRolloutW3SignoffState(
  state: TradeIbClientMigrationRolloutW3SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationRolloutW3ItemsVerified(
  state: TradeIbClientMigrationRolloutW3SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationRolloutW3VerificationCount(
  state: TradeIbClientMigrationRolloutW3SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W3_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationRolloutW3SignedOff(): boolean {
  return loadTradeIbClientMigrationRolloutW3SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationRolloutW3Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationRolloutW2SignedOff()) {
    missing.push('TIBM Rollout W2 sign-off required before W3 read-only API rollout sign-off')
  }
  return { ok: missing.length === 0, missing }
}
