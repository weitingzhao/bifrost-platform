/** TIBM Rollout prod — W1→W3 promote; daemon observe-safe; D10 still blocks live trading. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationRolloutDevComposeSignedOff } from './tradeIbClientMigrationRolloutDevComposeDelivery'

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_VERSION = '2026-07-04'

export interface TradeIbClientMigrationRolloutProdDeliveryItem {
  id:
    | 'TIBM-RP-1'
    | 'TIBM-RP-2'
    | 'TIBM-RP-3'
    | 'TIBM-RP-4'
    | 'TIBM-RP-5'
    | 'TIBM-RP-6'
    | 'TIBM-RP-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_DELIVERY_ITEMS: TradeIbClientMigrationRolloutProdDeliveryItem[] =
  [
    {
      id: 'TIBM-RP-1',
      title: 'dev-compose rollout complete',
      summary: 'STG + dev-compose env gates signed before prod promote.',
      verifySteps: [
        'Rollout dev-compose panel — DEV-COMPOSE SIGNED.',
        'STG rollout complete was signed earlier in the chain.',
      ],
    },
    {
      id: 'TIBM-RP-2',
      title: 'rollout-tibm-prod.sh',
      summary: 'Build + push :prod images; rollout W1→W3 only (monitor, ops, FE, celery-worker, read-only APIs).',
      verifySteps: [
        'make rollout-tibm-prod in bifrost-platform-plugin.',
        'Or Tekton deliver-prod equivalent with STG preflight gate.',
        'Daemon not rebuilt for live trading — observe-safe patch unchanged.',
      ],
    },
    {
      id: 'TIBM-RP-3',
      title: 'W1 + W2 + W3 prod runtime gates',
      summary: 'Observability, celery bars RPC, read-only API domains on bifrost-prod.',
      verifySteps: [
        'make verify-trade-ib-w1-prod',
        'make verify-trade-ib-w2-prod',
        'make verify-trade-ib-w3-prod',
      ],
    },
    {
      id: 'TIBM-RP-4',
      title: 'verify-trade-ib-rollout-prod.sh',
      summary: 'Aggregate prod gate — waves + program verify + core alignment + prod gateway.',
      verifySteps: [
        'make verify-trade-ib-rollout-prod in bifrost-platform-plugin.',
        'All six steps green before prod rollout sign-off.',
        'Prod gateway smoke uses Host: 192.168.10.70 (Traefik OR precedence on trade.bifrost.lan).',
      ],
    },
    {
      id: 'TIBM-RP-5',
      title: 'verify-trade-ib-migration-program',
      summary: 'RPC parity + health + Celery bars + UI relabel (4/4 program gates on cluster ACL).',
      verifySteps: [
        'Included in verify-trade-ib-rollout-prod step [2/6].',
        'trade-prod ACL on redis-ib for operator RPC write path.',
      ],
    },
    {
      id: 'TIBM-RP-6',
      title: 'D10 / observe-safe on prod',
      summary: 'Daemon runs observe mode (replicas>=1); legacy IB socket retired; no live orders.',
      verifySteps: [
        'kubectl get deploy daemon -n bifrost-prod — replicas >= 1 (observe-safe patch).',
        'No ib-market-gateway / ib-account-agent / ib-operator StatefulSets.',
        'Spine D10 still BLOCKED — W-block unchanged until Owner unlock.',
      ],
    },
    {
      id: 'TIBM-RP-7',
      title: 'prod Owner sign-off',
      summary: 'Confirm prod W1→W3 promote complete — TIBM rollout env chain done (no live trading).',
      verifySteps: [
        'Mark TIBM-RP-1 … TIBM-RP-6 verified.',
        'Sign off prod rollout (Admin token).',
        'Live trading remains BLOCKED until explicit D10 unlock program.',
      ],
    },
  ]

export interface TradeIbClientMigrationRolloutProdItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationRolloutProdSignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationRolloutProdItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_rollout_prod_signoff'

function emptyItemState(): TradeIbClientMigrationRolloutProdItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationRolloutProdSignoffState(): TradeIbClientMigrationRolloutProdSignoffState {
  const items: Record<string, TradeIbClientMigrationRolloutProdItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationRolloutProdSignoffState(): TradeIbClientMigrationRolloutProdSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationRolloutProdSignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationRolloutProdSignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_VERSION) {
      return defaultTradeIbClientMigrationRolloutProdSignoffState()
    }
    const merged = defaultTradeIbClientMigrationRolloutProdSignoffState()
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
    return defaultTradeIbClientMigrationRolloutProdSignoffState()
  }
}

export function saveTradeIbClientMigrationRolloutProdSignoffState(
  state: TradeIbClientMigrationRolloutProdSignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationRolloutProdItemsVerified(
  state: TradeIbClientMigrationRolloutProdSignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationRolloutProdVerificationCount(
  state: TradeIbClientMigrationRolloutProdSignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_PROD_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationRolloutProdSignedOff(): boolean {
  return loadTradeIbClientMigrationRolloutProdSignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationRolloutProdPrerequisites(): {
  ok: boolean
  missing: string[]
} {
  const missing: string[] = []
  if (!isTradeIbClientMigrationRolloutDevComposeSignedOff()) {
    missing.push('TIBM Rollout dev-compose sign-off required before prod rollout sign-off')
  }
  return { ok: missing.length === 0, missing }
}
