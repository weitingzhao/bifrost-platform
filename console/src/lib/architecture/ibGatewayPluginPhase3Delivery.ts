/** IB Gateway Plugin — Phase 3 delivery checklist (Trade cutover). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isIbGatewayPluginPhase2SignedOff } from './ibGatewayPluginPhase2Delivery'

export const IB_GATEWAY_PLUGIN_PHASE3_VERSION = '2026-07-04'

export interface IbGatewayPluginPhase3DeliveryItem {
  id: 'IBGP3-1' | 'IBGP3-2' | 'IBGP3-3' | 'IBGP3-4' | 'IBGP3-5' | 'IBGP3-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_PHASE3_DELIVERY_ITEMS: IbGatewayPluginPhase3DeliveryItem[] = [
  {
    id: 'IBGP3-1',
    title: 'Trade redis_ib config + ExternalName',
    summary:
      'Overlay configs point redis_ib.host → redis-ib; ACL username trade-dev (dev) / trade-prod (stg/prod).',
    verifySteps: [
      'k8s/overlays/{dev,stg,prod}/config — redis_ib block with host redis-ib.',
      'scripts/sync_redis_ib_trade_config.sh — passwords from bifrost-platform-plugin/.env.',
      'kubectl get svc redis-ib -n bifrost-dev — ExternalName → redis-ib.data.svc.cluster.local.',
    ],
  },
  {
    id: 'IBGP3-2',
    title: 'bifrost-core ib_redis_url_from_config',
    summary:
      'IB bus reads/writes use redis_ib when configured — operator client, account snapshot, ingestor tick reader.',
    verifySteps: [
      'bifrost_core.core.redis_url.ib_redis_url_from_config + REDIS_IB_* env fallbacks.',
      'ib_operator/config.py + portfolio/ib_edge.py + RedisQuotesReader IB client.',
      'bifrost-core bumped to v0.2.8.',
    ],
  },
  {
    id: 'IBGP3-3',
    title: 'Legacy IB StatefulSets retired',
    summary:
      'ib-market-gateway, ib-account-agent, ib-operator scaled to 0 in bifrost-{dev,stg,prod}.',
    verifySteps: [
      'k8s/overlays/*/ib-socket-retired.patch.yaml — replicas: 0 on all three StatefulSets.',
      'kubectl get sts -n bifrost-dev — legacy IB 0/0 ready.',
      'No parallel validation — Platform ib-gateway is sole TWS writer.',
    ],
  },
  {
    id: 'IBGP3-4',
    title: 'Trade reads Platform redis-ib bus',
    summary:
      'Monitor status + market quotes read bifrost:health:ws_ib_* and ib:ingester:tick:* from redis-ib.',
    verifySteps: [
      'make verify-trade-cutover in bifrost-platform-plugin — ACL ping + NVDA tick + operator ping.',
      'Trade monitor API ib_ingestor / ib_account_agent health from Platform gateway hashes.',
      'massive-ws unchanged — still on env-local redis.',
    ],
  },
  {
    id: 'IBGP3-5',
    title: 'platform-api cutover probe + Console panel',
    summary:
      'GET /plugins/ib-gateway/status includes cutover.environments — legacy replicas + ExternalName per NS.',
    verifySteps: [
      'status.cutover.legacy_socket_retired === true when all Trade NS at replicas=0.',
      'IbGatewayCutoverStatusPanel on Rocket → Cluster — per-env table.',
      'Live summary reflects cutover reachability.',
    ],
  },
  {
    id: 'IBGP3-6',
    title: 'Console Phase 3 sign-off panel',
    summary: 'IBGP3 checklist + Owner sign-off — completes IB Gateway Plugin program.',
    verifySteps: [
      'Phase 2 signed off; Phase 3 panel visible.',
      'Mark all IBGP3 items verified → Sign off Phase 3 (Admin token).',
      'Program strip 4/4 ✓ — optional live mode patch after full program sign-off.',
    ],
  },
]

export interface IbGatewayPluginPhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginPhase3SignoffState {
  version: string
  items: Record<string, IbGatewayPluginPhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_phase3_signoff'

function emptyItemState(): IbGatewayPluginPhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginPhase3SignoffState(): IbGatewayPluginPhase3SignoffState {
  const items: Record<string, IbGatewayPluginPhase3ItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginPhase3SignoffState(): IbGatewayPluginPhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginPhase3SignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginPhase3SignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_PHASE3_VERSION) {
      return defaultIbGatewayPluginPhase3SignoffState()
    }
    const merged = defaultIbGatewayPluginPhase3SignoffState()
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
    return defaultIbGatewayPluginPhase3SignoffState()
  }
}

export function saveIbGatewayPluginPhase3SignoffState(state: IbGatewayPluginPhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginPhase3ItemsVerified(state: IbGatewayPluginPhase3SignoffState): boolean {
  return IB_GATEWAY_PLUGIN_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginPhase3VerificationCount(state: IbGatewayPluginPhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_PHASE3_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginPhase3SignedOff(): boolean {
  return loadIbGatewayPluginPhase3SignoffState().signedOffAt != null
}

export function priorIbGatewayPluginPhase3Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isIbGatewayPluginPhase2SignedOff()) {
    missing.push('IBGP2 Phase 2 sign-off required')
  }
  return { ok: missing.length === 0, missing }
}
