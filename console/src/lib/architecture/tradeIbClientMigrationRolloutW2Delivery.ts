/** TIBM Rollout W2 — STG data plane (celery-worker / stocks_ib bars). D10: no daemon. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationRolloutW1SignedOff } from './tradeIbClientMigrationRolloutW1Delivery'

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_VERSION = '2026-07-04'

export interface TradeIbClientMigrationRolloutW2DeliveryItem {
  id: 'TIBM-W2-1' | 'TIBM-W2-2' | 'TIBM-W2-3' | 'TIBM-W2-4' | 'TIBM-W2-5' | 'TIBM-W2-6' | 'TIBM-W2-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_DELIVERY_ITEMS: TradeIbClientMigrationRolloutW2DeliveryItem[] =
  [
    {
      id: 'TIBM-W2-1',
      title: 'STG celery-worker image rebuilt',
      summary: 'bifrost-worker :stg with bifrost-core >= 0.2.10 — IbOperatorBarsAdapter bars transport.',
      verifySteps: [
        'make rollout-tibm-w2-stg in bifrost-platform-plugin.',
        'Registry 192.168.10.73:30500 — bifrost-worker:stg pushed (linux/amd64).',
        'Pod pip show bifrost-core Version >= 0.2.10.',
      ],
    },
    {
      id: 'TIBM-W2-2',
      title: 'Rollout restart celery-worker only',
      summary: 'celery-worker available in bifrost-stg; daemon stays replicas:0 (D10).',
      verifySteps: [
        'kubectl rollout status deployment/celery-worker -n bifrost-stg',
        'kubectl get deploy daemon -n bifrost-stg — replicas 0',
        'No daemon or trading-engine scale-up during W2.',
      ],
    },
    {
      id: 'TIBM-W2-3',
      title: 'IbOperatorBarsAdapter runtime',
      summary: 'Worker loads Platform Gateway bars adapter; ib_operator.use_for_celery_bars true in STG config.',
      verifySteps: [
        'kubectl exec deploy/celery-worker — import bifrost_worker.data.bars.ib_operator_transport.',
        'effective_ib_operator_settings → use_for_celery_bars true.',
        'make verify-trade-ib-w2-stg step [4/7].',
      ],
    },
    {
      id: 'TIBM-W2-4',
      title: 'verify-trade-celery-bars',
      summary: 'Operator ping + fetch_bars_range SPY via trade-prod ACL on redis-ib (mock or live Gateway).',
      verifySteps: [
        'make verify-trade-celery-bars in bifrost-platform-plugin.',
        'ib:operator:cmd ping + fetch_bars_range return ok.',
        'Gateway pod ready in data namespace.',
      ],
    },
    {
      id: 'TIBM-W2-5',
      title: 'No direct TWS in bars worker',
      summary: 'stocks_ib Celery path — no MarketIbClient / ib_insync socket from worker pod.',
      verifySteps: [
        'grep tasks.py — no MarketIbClient / ib_insync.',
        'Bars backfill uses fetch_bars_range RPC only (TIBM3).',
        'make verify-trade-ib-w2-stg step [7/7].',
      ],
    },
    {
      id: 'TIBM-W2-6',
      title: 'Trade Ops Celery UI (optional)',
      summary: 'Operations → Celery — worker instance visible; stocks_ib queue reachable after rollout.',
      verifySteps: [
        'Trade STG → Operations → Celery.',
        'Worker profile stocks_ib listed; no crash loop on celery-worker pod.',
        'Historical bars jobs use Platform Gateway — not live order placement.',
      ],
    },
    {
      id: 'TIBM-W2-7',
      title: 'W2 Owner sign-off',
      summary: 'Confirm STG data-plane rollout complete — D10 still blocks live trading.',
      verifySteps: [
        'Mark TIBM-W2-1 … TIBM-W2-6 verified.',
        'Sign off W2 (Admin token).',
        'Proceed to W3 (read-only API domains) only after W2 signed.',
      ],
    },
  ]

export interface TradeIbClientMigrationRolloutW2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationRolloutW2SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationRolloutW2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_rollout_w2_signoff'

function emptyItemState(): TradeIbClientMigrationRolloutW2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationRolloutW2SignoffState(): TradeIbClientMigrationRolloutW2SignoffState {
  const items: Record<string, TradeIbClientMigrationRolloutW2ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationRolloutW2SignoffState(): TradeIbClientMigrationRolloutW2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationRolloutW2SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationRolloutW2SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_VERSION) {
      return defaultTradeIbClientMigrationRolloutW2SignoffState()
    }
    const merged = defaultTradeIbClientMigrationRolloutW2SignoffState()
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
    return defaultTradeIbClientMigrationRolloutW2SignoffState()
  }
}

export function saveTradeIbClientMigrationRolloutW2SignoffState(
  state: TradeIbClientMigrationRolloutW2SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationRolloutW2ItemsVerified(
  state: TradeIbClientMigrationRolloutW2SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationRolloutW2VerificationCount(
  state: TradeIbClientMigrationRolloutW2SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W2_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationRolloutW2SignedOff(): boolean {
  return loadTradeIbClientMigrationRolloutW2SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationRolloutW2Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationRolloutW1SignedOff()) {
    missing.push('TIBM Rollout W1 sign-off required before W2 data-plane rollout sign-off')
  }
  return { ok: missing.length === 0, missing }
}
