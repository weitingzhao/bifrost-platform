/** TIBM Rollout dev-compose — local make dev W1+W2; trade-dev ACL; D10 still blocks trading. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationRolloutStgSignedOff } from './tradeIbClientMigrationRolloutStgDelivery'

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_VERSION = '2026-07-04'

export interface TradeIbClientMigrationRolloutDevComposeDeliveryItem {
  id:
    | 'TIBM-RDC-1'
    | 'TIBM-RDC-2'
    | 'TIBM-RDC-3'
    | 'TIBM-RDC-4'
    | 'TIBM-RDC-5'
    | 'TIBM-RDC-6'
    | 'TIBM-RDC-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_DELIVERY_ITEMS: TradeIbClientMigrationRolloutDevComposeDeliveryItem[] =
  [
    {
      id: 'TIBM-RDC-1',
      title: 'STG rollout complete',
      summary: 'W1→W3 STG waves + STG rollout gate signed before dev-compose.',
      verifySteps: [
        'Rollout STG complete panel — STG ROLLOUT SIGNED.',
        'STG verify-trade-ib-rollout-stg was green at sign-off.',
      ],
    },
    {
      id: 'TIBM-RDC-2',
      title: 'redis_ib @ config.dev.yaml',
      summary:
        'Local compose reaches Platform IB bus (host.docker.internal → redis-ib). trade-prod ACL for RPC write path; trade-dev read verified separately.',
      verifySteps: [
        'make sync-redis-ib-dev-compose-config in bifrost-platform-plugin.',
        'config.dev.yaml redis_ib block present (default username trade-prod for celery RPC).',
        'kubectl port-forward -n data svc/redis-ib 6380:6379 (default REDIS_IB_PORT; avoids host :6379 Trade Redis).',
      ],
    },
    {
      id: 'TIBM-RDC-3',
      title: 'rollout-tibm-dev-compose.sh',
      summary: 'Restart api-monitor, api-ops, celery-worker, frontend; stop daemon + legacy ib-* services.',
      verifySteps: [
        'make rollout-tibm-dev-compose in bifrost-platform-plugin.',
        'docker compose ps — W1+W2 up; daemon/ib-ingestor/ib-operator/ib-account-agent stopped.',
      ],
    },
    {
      id: 'TIBM-RDC-4',
      title: 'verify-trade-ib-migration-program-dev',
      summary: 'trade-dev read ACL + full program 4/4 (trade-prod RPC for operator/celery write path).',
      verifySteps: [
        'make verify-trade-ib-migration-program-dev.',
        'trade-dev: PING + health read; trade-prod: RPC parity + Celery bars + UI.',
      ],
    },
    {
      id: 'TIBM-RDC-5',
      title: 'verify-trade-ib-rollout-dev-compose.sh',
      summary: 'Aggregate dev-compose gate — config + compose runtime + local Monitor /status.',
      verifySteps: [
        'make verify-trade-ib-rollout-dev-compose.',
        'Local GET /status — socket.platform_ib_gateway present.',
      ],
    },
    {
      id: 'TIBM-RDC-6',
      title: 'D10 / legacy socket retired on dev-compose',
      summary: 'No local daemon auto-trade; no Trade IB socket sidecars in compose profile.',
      verifySteps: [
        'compose: daemon replicas 0 / not running.',
        'Legacy ib-ingestor / ib-operator / ib-account-agent not running.',
        'Spine D10 still BLOCKED — no live order path.',
      ],
    },
    {
      id: 'TIBM-RDC-7',
      title: 'dev-compose Owner sign-off',
      summary: 'Confirm local dev stack aligned to Platform Gateway — ready for prod promote (no live trading).',
      verifySteps: [
        'Mark TIBM-RDC-1 … TIBM-RDC-6 verified.',
        'Sign off dev-compose rollout (Admin token).',
        'Next env step: prod promote (W1–W3 only, D10 still BLOCKED).',
      ],
    },
  ]

export interface TradeIbClientMigrationRolloutDevComposeItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationRolloutDevComposeSignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationRolloutDevComposeItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_rollout_dev_compose_signoff'

function emptyItemState(): TradeIbClientMigrationRolloutDevComposeItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationRolloutDevComposeSignoffState(): TradeIbClientMigrationRolloutDevComposeSignoffState {
  const items: Record<string, TradeIbClientMigrationRolloutDevComposeItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationRolloutDevComposeSignoffState(): TradeIbClientMigrationRolloutDevComposeSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationRolloutDevComposeSignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationRolloutDevComposeSignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_VERSION) {
      return defaultTradeIbClientMigrationRolloutDevComposeSignoffState()
    }
    const merged = defaultTradeIbClientMigrationRolloutDevComposeSignoffState()
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
    return defaultTradeIbClientMigrationRolloutDevComposeSignoffState()
  }
}

export function saveTradeIbClientMigrationRolloutDevComposeSignoffState(
  state: TradeIbClientMigrationRolloutDevComposeSignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationRolloutDevComposeItemsVerified(
  state: TradeIbClientMigrationRolloutDevComposeSignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationRolloutDevComposeVerificationCount(
  state: TradeIbClientMigrationRolloutDevComposeSignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_DEV_COMPOSE_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationRolloutDevComposeSignedOff(): boolean {
  return loadTradeIbClientMigrationRolloutDevComposeSignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationRolloutDevComposePrerequisites(): {
  ok: boolean
  missing: string[]
} {
  const missing: string[] = []
  if (!isTradeIbClientMigrationRolloutStgSignedOff()) {
    missing.push('TIBM Rollout STG complete sign-off required before dev-compose sign-off')
  }
  return { ok: missing.length === 0, missing }
}
