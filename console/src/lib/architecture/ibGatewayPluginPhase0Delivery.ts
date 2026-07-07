/** IB Gateway Plugin — Phase 0 delivery checklist (redis-ib infrastructure). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const IB_GATEWAY_PLUGIN_PHASE0_VERSION = '2026-07-04'

export interface IbGatewayPluginPhase0DeliveryItem {
  id: 'IBGP0-1' | 'IBGP0-2' | 'IBGP0-3' | 'IBGP0-4' | 'IBGP0-5' | 'IBGP0-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_PHASE0_DELIVERY_ITEMS: IbGatewayPluginPhase0DeliveryItem[] = [
  {
    id: 'IBGP0-1',
    title: 'bifrost-platform-plugin repo skeleton',
    summary:
      'Independent plugin repo with pyproject.toml, bifrost_plugin.ib_gateway package stub, CLAUDE.md, Makefile.',
    verifySteps: [
      'Repo path bifrost-platform-plugin/ exists with src/bifrost_plugin/ib_gateway/config.py.',
      'make install-dev && make test pass (redis key contract smoke test).',
      'Plugin boundary documented — not part of bifrost-platform core.',
    ],
  },
  {
    id: 'IBGP0-2',
    title: 'redis-ib K8s manifests @ data NS',
    summary:
      'Deployment + Service + PDB — ephemeral Redis (no AOF/RDB), maxmemory 256MB, LRU eviction.',
    verifySteps: [
      'k8s/redis-ib/deployment.yaml — redis:7-alpine, --save "" --appendonly no.',
      'make install-redis-ib applies secret + kustomize to data NS.',
      'kubectl get pods,svc,pdb -n data -l app.kubernetes.io/name=redis-ib — all ready.',
    ],
  },
  {
    id: 'IBGP0-3',
    title: 'Redis ACL users',
    summary:
      'ib-gateway (full), trade-prod (read/write), trade-dev (read-only operator), platform (health/control).',
    verifySteps: [
      'scripts/install-redis-ib.sh renders acl.conf from .env passwords.',
      'trade-dev: XADD ib:operator:cmd → NOPERM; GET ib:ingester:tick:* → OK.',
      'trade-prod: write ib:operator:cmd → OK.',
    ],
  },
  {
    id: 'IBGP0-4',
    title: 'NetworkPolicy + PDB',
    summary:
      'Ingress from data, bifrost-{dev,stg,prod}, bifrost-platform-{stg,prod}; maxUnavailable: 0.',
    verifySteps: [
      'k8s/redis-ib/network-policy.yaml allows listed namespaces on TCP 6379.',
      'PDB redis-ib maxUnavailable: 0 — node drain cannot evict without override.',
    ],
  },
  {
    id: 'IBGP0-5',
    title: 'Trade NS ExternalName aliases',
    summary: 'redis-ib short name in bifrost-{dev,stg,prod} → redis-ib.data.svc.cluster.local.',
    verifySteps: [
      'make apply-external-names creates ExternalName Service in each Trade NS.',
      'From bifrost-prod pod: redis-cli -h redis-ib PING → PONG.',
    ],
  },
  {
    id: 'IBGP0-6',
    title: 'Console Plugin page + Phase 0 sign-off',
    summary:
      'Delivery Board · ib-gateway-plugin — program strip, catalog tables, Phase 0 sign-off panel.',
    verifySteps: [
      'Navigate Mission Control → Delivery Board → IB Gateway Plugin.',
      'Mark all IBGP0 items verified → Sign off Phase 0 delivery (Admin token).',
      'Signed state persists in localStorage; program strip shows IBGP0 ✓.',
    ],
  },
]

export interface IbGatewayPluginPhase0ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginPhase0SignoffState {
  version: string
  items: Record<string, IbGatewayPluginPhase0ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_phase0_signoff'

function emptyItemState(): IbGatewayPluginPhase0ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginPhase0SignoffState(): IbGatewayPluginPhase0SignoffState {
  const items: Record<string, IbGatewayPluginPhase0ItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_PHASE0_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_PHASE0_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginPhase0SignoffState(): IbGatewayPluginPhase0SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginPhase0SignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginPhase0SignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_PHASE0_VERSION) {
      return defaultIbGatewayPluginPhase0SignoffState()
    }
    const merged = defaultIbGatewayPluginPhase0SignoffState()
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
    return defaultIbGatewayPluginPhase0SignoffState()
  }
}

export function saveIbGatewayPluginPhase0SignoffState(state: IbGatewayPluginPhase0SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginPhase0ItemsVerified(state: IbGatewayPluginPhase0SignoffState): boolean {
  return IB_GATEWAY_PLUGIN_PHASE0_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginPhase0VerificationCount(state: IbGatewayPluginPhase0SignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_PHASE0_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_PHASE0_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginPhase0SignedOff(): boolean {
  return loadIbGatewayPluginPhase0SignoffState().signedOffAt != null
}

export function priorIbGatewayPluginPhase0Prerequisites(): { ok: boolean; missing: string[] } {
  return { ok: true, missing: [] }
}
