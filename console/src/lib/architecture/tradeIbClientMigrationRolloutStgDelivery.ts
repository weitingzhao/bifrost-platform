/** TIBM Rollout STG complete — W0–W3 waves signed; D10 still blocks trading. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationRolloutW3SignedOff } from './tradeIbClientMigrationRolloutW3Delivery'

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_VERSION = '2026-07-04'

export interface TradeIbClientMigrationRolloutStgDeliveryItem {
  id:
    | 'TIBM-RS-1'
    | 'TIBM-RS-2'
    | 'TIBM-RS-3'
    | 'TIBM-RS-4'
    | 'TIBM-RS-5'
    | 'TIBM-RS-6'
    | 'TIBM-RS-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_DELIVERY_ITEMS: TradeIbClientMigrationRolloutStgDeliveryItem[] =
  [
    {
      id: 'TIBM-RS-1',
      title: 'W1 observability wave',
      summary: 'api-monitor, api-ops, frontend on bifrost-core >= 0.2.10; W1 signed.',
      verifySteps: [
        'make verify-trade-ib-w1-stg passes.',
        'Rollout W1 sign-off panel — W1 SIGNED.',
      ],
    },
    {
      id: 'TIBM-RS-2',
      title: 'W2 data plane wave',
      summary: 'celery-worker bars via Platform IB Gateway RPC; W2 signed.',
      verifySteps: [
        'make verify-trade-ib-w2-stg passes.',
        'Rollout W2 sign-off panel — W2 SIGNED.',
      ],
    },
    {
      id: 'TIBM-RS-3',
      title: 'W3 read-only API wave',
      summary: 'market, massive, research, portfolio, docs, trading APIs rolled; W3 signed.',
      verifySteps: [
        'make verify-trade-ib-w3-stg passes.',
        'Rollout W3 sign-off panel — W3 SIGNED.',
        'api-strategy intentionally deferred (not in W3 scope).',
      ],
    },
    {
      id: 'TIBM-RS-4',
      title: 'verify-trade-ib-rollout-stg.sh',
      summary: 'Aggregate STG rollout gate — W1+W2+W3 + program verify + core alignment.',
      verifySteps: [
        'make verify-trade-ib-rollout-stg in bifrost-platform-plugin.',
        'All five steps green before STG rollout sign-off.',
      ],
    },
    {
      id: 'TIBM-RS-5',
      title: 'verify-trade-ib-migration-program',
      summary: 'RPC parity + health + Celery bars + UI relabel (4/4 program gates).',
      verifySteps: [
        'Included in verify-trade-ib-rollout-stg step [2/5].',
        'Gateway mock OK — live TWS not required for STG rollout complete.',
      ],
    },
    {
      id: 'TIBM-RS-6',
      title: 'D10 / W-block acknowledged',
      summary: 'daemon replicas=0 on STG; no live order execution; spine D10 BLOCKED.',
      verifySteps: [
        'kubectl get deploy daemon -n bifrost-stg — replicas 0.',
        'No Owner D10 UNLOCKED in spine.',
        'W-block targets remain out of scope until explicit unlock program.',
      ],
    },
    {
      id: 'TIBM-RS-7',
      title: 'STG rollout Owner sign-off',
      summary: 'Confirm W1→W3 STG deployment complete — ready for soak / dev-compose / prod promote (no live trading).',
      verifySteps: [
        'Mark TIBM-RS-1 … TIBM-RS-6 verified.',
        'Sign off STG rollout (Admin token).',
        'Next env step per catalog: dev-compose or prod promote — still no D10 unlock.',
      ],
    },
  ]

export interface TradeIbClientMigrationRolloutStgItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationRolloutStgSignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationRolloutStgItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_rollout_stg_signoff'

function emptyItemState(): TradeIbClientMigrationRolloutStgItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationRolloutStgSignoffState(): TradeIbClientMigrationRolloutStgSignoffState {
  const items: Record<string, TradeIbClientMigrationRolloutStgItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationRolloutStgSignoffState(): TradeIbClientMigrationRolloutStgSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationRolloutStgSignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationRolloutStgSignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_VERSION) {
      return defaultTradeIbClientMigrationRolloutStgSignoffState()
    }
    const merged = defaultTradeIbClientMigrationRolloutStgSignoffState()
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
    return defaultTradeIbClientMigrationRolloutStgSignoffState()
  }
}

export function saveTradeIbClientMigrationRolloutStgSignoffState(
  state: TradeIbClientMigrationRolloutStgSignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationRolloutStgItemsVerified(
  state: TradeIbClientMigrationRolloutStgSignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationRolloutStgVerificationCount(
  state: TradeIbClientMigrationRolloutStgSignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_STG_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationRolloutStgSignedOff(): boolean {
  return loadTradeIbClientMigrationRolloutStgSignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationRolloutStgPrerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationRolloutW3SignedOff()) {
    missing.push('TIBM Rollout W3 sign-off required before STG rollout complete sign-off')
  }
  return { ok: missing.length === 0, missing }
}
