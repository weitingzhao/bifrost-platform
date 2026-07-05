/** IB Gateway Plugin — Phase 1 delivery checklist (Gateway core + K8s deploy). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isIbGatewayPluginPhase0SignedOff } from './ibGatewayPluginPhase0Delivery'

export const IB_GATEWAY_PLUGIN_PHASE1_VERSION = '2026-07-04'

export interface IbGatewayPluginPhase1DeliveryItem {
  id: 'IBGP1-1' | 'IBGP1-2' | 'IBGP1-3' | 'IBGP1-4' | 'IBGP1-5' | 'IBGP1-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_PHASE1_DELIVERY_ITEMS: IbGatewayPluginPhase1DeliveryItem[] = [
  {
    id: 'IBGP1-1',
    title: 'IB Gateway core package',
    summary:
      'Single-process Gateway — Host + Secondary TWS slots, tick/account writer, operator RPC consumer, mock/live modes.',
    verifySteps: [
      'src/bifrost_plugin/ib_gateway/{app,live,mock,operator,writer,connection,redis_keys,protocol}.py present.',
      'make install-dev && make test — protocol + redis key contract tests pass.',
      'Legacy Redis keys unchanged: ib:ingester:tick:*, ib:account:snapshot:v1, ib:operator:cmd.',
    ],
  },
  {
    id: 'IBGP1-2',
    title: 'Operator RPC consumer (group ib-gateway)',
    summary:
      'Stream consumer on ib:operator:cmd — ping, fetch_bars; results on ib:operator:result:{req_id} (TTL 300).',
    verifySteps: [
      'Consumer group ib-gateway (not legacy ib-operator).',
      'make verify-ib-gateway — XADD ping → result JSON with ok:true within 2s.',
      'trade-prod ACL can XADD cmd; trade-dev read-only on operator stream.',
    ],
  },
  {
    id: 'IBGP1-3',
    title: 'Mock mode tick + health pipelines',
    summary:
      'MockGateway writes synthetic NVDA tick + legacy bifrost:health:ws_ib_* hashes without TWS.',
    verifySteps: [
      'ConfigMap gateway.yaml mode: mock (default deploy).',
      'redis GET ib:ingester:tick:NVDA returns JSON with bid/ask/last.',
      'HGETALL bifrost:health:ws_ib_ingestor shows status=ok.',
    ],
  },
  {
    id: 'IBGP1-4',
    title: 'K8s ib-gateway Deployment @ data NS',
    summary:
      'Deployment + ConfigMap + NetworkPolicy (egress TWS .30/.32:7496 + redis-ib) + PDB maxUnavailable:0.',
    verifySteps: [
      'make install-ib-gateway — docker build + deploy to data NS.',
      'kubectl get deploy,pod -n data -l app.kubernetes.io/name=ib-gateway — 1/1 ready.',
      'NetworkPolicy allows egress to 192.168.10.30/32:7496 and redis-ib.',
    ],
  },
  {
    id: 'IBGP1-5',
    title: 'Live mode readiness (TWS config)',
    summary:
      'config/gateway.yaml documents Host wzhao1503 @ .30:7496, Secondary vzhao1503 @ .32:7496 — patch to live when TWS ready.',
    verifySteps: [
      'ConfigMap includes tws.host / tws.secondary blocks with client_id slots [1,2].',
      'kubectl patch configmap ib-gateway-config -n data --type merge -p \'{"data":{"mode":"live"}}\' when ready.',
      'Live: ib:health:{account_id} + real account snapshot on ib:account:snapshot:v1.',
    ],
  },
  {
    id: 'IBGP1-6',
    title: 'Console Phase 1 sign-off panel',
    summary:
      'Delivery Board → IB Gateway Plugin — Phase 1 checklist + sign-off after cluster verify.',
    verifySteps: [
      'Phase 0 signed off; Phase 1 panel visible below Phase 0.',
      'Mark all IBGP1 items verified → Sign off Phase 1 delivery (Admin token).',
      'Program strip shows IBGP1 ✓ — proceed to Phase 2 Platform API.',
    ],
  },
]

export interface IbGatewayPluginPhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginPhase1SignoffState {
  version: string
  items: Record<string, IbGatewayPluginPhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_phase1_signoff'

function emptyItemState(): IbGatewayPluginPhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginPhase1SignoffState(): IbGatewayPluginPhase1SignoffState {
  const items: Record<string, IbGatewayPluginPhase1ItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginPhase1SignoffState(): IbGatewayPluginPhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginPhase1SignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginPhase1SignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_PHASE1_VERSION) {
      return defaultIbGatewayPluginPhase1SignoffState()
    }
    const merged = defaultIbGatewayPluginPhase1SignoffState()
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
    return defaultIbGatewayPluginPhase1SignoffState()
  }
}

export function saveIbGatewayPluginPhase1SignoffState(state: IbGatewayPluginPhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginPhase1ItemsVerified(state: IbGatewayPluginPhase1SignoffState): boolean {
  return IB_GATEWAY_PLUGIN_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginPhase1VerificationCount(state: IbGatewayPluginPhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_PHASE1_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginPhase1SignedOff(): boolean {
  return loadIbGatewayPluginPhase1SignoffState().signedOffAt != null
}

export function priorIbGatewayPluginPhase1Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isIbGatewayPluginPhase0SignedOff()) {
    missing.push('IBGP0 Phase 0 sign-off required')
  }
  return { ok: missing.length === 0, missing }
}
