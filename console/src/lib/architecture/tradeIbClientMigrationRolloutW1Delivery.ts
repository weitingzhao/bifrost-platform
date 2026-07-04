/** TIBM Rollout W1 — STG observability plane (monitor, ops, frontend). D10: no daemon. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isTradeIbClientMigrationProgramSignedOff } from './tradeIbClientMigrationProgramDelivery'

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_VERSION = '2026-07-04'

export interface TradeIbClientMigrationRolloutW1DeliveryItem {
  id: 'TIBM-W1-1' | 'TIBM-W1-2' | 'TIBM-W1-2b' | 'TIBM-W1-3' | 'TIBM-W1-4' | 'TIBM-W1-5' | 'TIBM-W1-6' | 'TIBM-W1-7'
  title: string
  summary: string
  verifySteps: string[]
}

export const TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS: TradeIbClientMigrationRolloutW1DeliveryItem[] =
  [
    {
      id: 'TIBM-W1-1',
      title: 'STG images rebuilt',
      summary: 'bifrost-api-monitor, bifrost-api-ops, bifrost-frontend :stg with bifrost-core >= 0.2.10.',
      verifySteps: [
        'make rollout-tibm-w1-stg in bifrost-platform-plugin (or Tekton deliver-stg equivalent).',
        'Registry 192.168.10.73:30500 — images pushed with platform_ib_gateway module.',
      ],
    },
    {
      id: 'TIBM-W1-2',
      title: 'Rollout restart W1 deployments',
      summary: 'api-monitor, api-ops, frontend available in bifrost-stg; daemon stays replicas:0 (D10).',
      verifySteps: [
        'kubectl rollout status deployment/api-monitor -n bifrost-stg',
        'kubectl rollout status deployment/api-ops -n bifrost-stg',
        'kubectl rollout status deployment/frontend -n bifrost-stg',
        'kubectl get deploy daemon -n bifrost-stg — replicas 0',
        'Images linux/amd64 — Mac builds use --platform linux/amd64 + crane push --insecure',
      ],
    },
    {
      id: 'TIBM-W1-2b',
      title: 'redis-ib ACL — trade-prod health read',
      summary:
        'trade-prod ACL includes ~bifrost:health:ws_ib_* so Monitor can aggregate platform_ib_gateway.',
      verifySteps: [
        'acl.conf.example trade-prod: ~ib:* ~bifrost:health:ws_ib_*',
        'make install-redis-ib + redis-ib rollout restart in data NS',
        'api-monitor pod: HGETALL bifrost:health:ws_ib_ingestor succeeds (no NoPermissionError)',
      ],
    },
    {
      id: 'TIBM-W1-3',
      title: 'Monitor /status platform_ib_gateway',
      summary: 'Runtime — GET /api/monitor/status exposes socket.platform_ib_gateway aggregate.',
      verifySteps: [
        'curl -H Host:trade-stg.bifrost.lan http://192.168.10.73/api/monitor/status',
        'JSON path socket.platform_ib_gateway non-null (lamp + transport tags).',
        'make verify-trade-ib-w1-stg step [4/6].',
      ],
    },
    {
      id: 'TIBM-W1-4',
      title: 'Ops market-ingest Platform Gateway',
      summary: 'GET /ops/market-ingest/services — platform_gateway_managed when health plugin=ib-gateway.',
      verifySteps: [
        'Ops API returns IB rows with Platform IB Gateway labels.',
        'platform_gateway_managed true when Gateway health green (mock or live).',
      ],
    },
    {
      id: 'TIBM-W1-5',
      title: 'verify-trade-ib-w1-stg.sh',
      summary: 'Automated W1 runtime gate — deployments, D10 daemon guard, Monitor status, TIBM4 health subset.',
      verifySteps: [
        'make verify-trade-ib-w1-stg in bifrost-platform-plugin.',
        'All six steps green before Owner sign-off.',
      ],
    },
    {
      id: 'TIBM-W1-6',
      title: 'Trade FE Socket ingest UI',
      summary: 'Settings → Socket — Platform IB Gateway labels; aggregate lamp from platform_ib_gateway.',
      verifySteps: [
        'Open Trade STG UI → Settings → Socket ingest / IB Connection.',
        'Category labels say Platform IB Gateway @ redis-ib — not legacy trade-socket STS.',
        'Health lamps reflect mock/live Gateway mode from Monitor /status.',
      ],
    },
    {
      id: 'TIBM-W1-7',
      title: 'W1 Owner sign-off',
      summary: 'Confirm STG observability rollout complete — D10 still blocks live trading.',
      verifySteps: [
        'Mark TIBM-W1-1 … TIBM-W1-6 verified.',
        'Sign off W1 (Admin token).',
        'Proceed to W2 (celery-worker) only after W1 signed.',
      ],
    },
  ]

export interface TradeIbClientMigrationRolloutW1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface TradeIbClientMigrationRolloutW1SignoffState {
  version: string
  items: Record<string, TradeIbClientMigrationRolloutW1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_trade_ib_client_migration_rollout_w1_signoff'

function emptyItemState(): TradeIbClientMigrationRolloutW1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultTradeIbClientMigrationRolloutW1SignoffState(): TradeIbClientMigrationRolloutW1SignoffState {
  const items: Record<string, TradeIbClientMigrationRolloutW1ItemVerification> = {}
  for (const item of TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadTradeIbClientMigrationRolloutW1SignoffState(): TradeIbClientMigrationRolloutW1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultTradeIbClientMigrationRolloutW1SignoffState()
    const parsed = JSON.parse(raw) as TradeIbClientMigrationRolloutW1SignoffState
    if (parsed.version !== TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_VERSION) {
      return defaultTradeIbClientMigrationRolloutW1SignoffState()
    }
    const merged = defaultTradeIbClientMigrationRolloutW1SignoffState()
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
    return defaultTradeIbClientMigrationRolloutW1SignoffState()
  }
}

export function saveTradeIbClientMigrationRolloutW1SignoffState(
  state: TradeIbClientMigrationRolloutW1SignoffState,
): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allTradeIbClientMigrationRolloutW1ItemsVerified(
  state: TradeIbClientMigrationRolloutW1SignoffState,
): boolean {
  return TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS.every(
    item => state.items[item.id]?.verified === true,
  )
}

export function tradeIbClientMigrationRolloutW1VerificationCount(
  state: TradeIbClientMigrationRolloutW1SignoffState,
): { verified: number; total: number } {
  const verified = TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: TRADE_IB_CLIENT_MIGRATION_ROLLOUT_W1_DELIVERY_ITEMS.length }
}

export function isTradeIbClientMigrationRolloutW1SignedOff(): boolean {
  return loadTradeIbClientMigrationRolloutW1SignoffState().signedOffAt != null
}

export function priorTradeIbClientMigrationRolloutW1Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isTradeIbClientMigrationProgramSignedOff()) {
    missing.push('TIBM program completion sign-off (TIBM-PC) required before W1 rollout sign-off')
  }
  return { ok: missing.length === 0, missing }
}
