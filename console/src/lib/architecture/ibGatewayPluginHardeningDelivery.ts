/** IB Gateway Plugin — post-program hardening / terminalization delivery. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isIbGatewayPluginProgramSignedOff } from './ibGatewayPluginProgramDelivery'

export const IB_GATEWAY_PLUGIN_HARDENING_VERSION = '2026-07-04'

export interface IbGatewayPluginHardeningDeliveryItem {
  id: 'IBGH-1' | 'IBGH-2' | 'IBGH-3' | 'IBGH-4' | 'IBGH-5' | 'IBGH-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_HARDENING_DELIVERY_ITEMS: IbGatewayPluginHardeningDeliveryItem[] = [
  {
    id: 'IBGH-1',
    title: 'Legacy IB removed from Trade kustomize',
    summary:
      'ib-market-gateway / ib-account-agent / ib-operator no longer in base or overlays; manifests archived under k8s/legacy/ib-socket/.',
    verifySteps: [
      'kubectl kustomize bifrost-trade-infra/k8s/overlays/{dev,stg,prod} — no IB StatefulSets.',
      'scripts/k3s/retire-legacy-ib-socket.sh — delete STS; suspend Argo until git push.',
      'Push infra k8s changes → re-enable Argo sync (prune removes orphans).',
      'Only massive-ws remains in socket manifest.',
    ],
  },
  {
    id: 'IBGH-2',
    title: 'Tekton deliver-stg drift fix',
    summary: 'task-deliver-stg no longer rollout-restarts legacy IB StatefulSets (prevents stg replicas=1 drift).',
    verifySteps: [
      'k8s/cicd/tekton/task-deliver-stg.yaml — no ib-market-gateway / ib-account-agent / ib-operator restart.',
      'After deliver-stg, legacy IB stays absent/retired.',
    ],
  },
  {
    id: 'IBGH-3',
    title: 'Unified redis-ib secret sync',
    summary: 'scripts/sync_redis_ib_secrets.sh — plugin .env → Trade overlay configs + platform .env.',
    verifySteps: [
      'make sync-redis-ib-secrets in bifrost-platform-plugin.',
      'Trade config.*.yaml redis_ib passwords match plugin .env.',
      'platform/.env REDIS_IB_PLATFORM_PASS aligned.',
    ],
  },
  {
    id: 'IBGH-4',
    title: 'Verify scripts hardened',
    summary: 'Operator ping poll retry; verify-trade-cutover includes bifrost-stg ACL + tick read.',
    verifySteps: [
      'scripts/lib/redis_operator_ping.sh — 15s poll instead of fixed sleep.',
      'verify-trade-cutover.sh step 4 uses ib:ingester:tick:NVDA|STK|||.',
      'verify-trade-quotes-e2e.sh — redis canonical key + Market API /quotes.',
    ],
  },
  {
    id: 'IBGH-5',
    title: 'Compose + k3s verify legacy expectations',
    summary: 'docker-compose IB socket services behind legacy-ib profile; k3s verify scripts expect legacy absent.',
    verifySteps: [
      'docker compose — ib-ingestor/account-agent/operator require profile legacy-ib.',
      'verify-w11 / verify-phase-b-stg-v2 — legacy STS absent OK, active replicas WARN/FAIL.',
    ],
  },
  {
    id: 'IBGH-6',
    title: 'Full program verify green',
    summary: 'make verify-ib-gateway-program passes after hardening — cutover + live + status aggregate.',
    verifySteps: [
      'make verify-ib-gateway-program in bifrost-platform-plugin.',
      'All envs legacy_socket_retired=true, redis_ib_external_name_ok=true.',
      'Live TWS slots connected; operator ping OK.',
    ],
  },
]

export interface IbGatewayPluginHardeningItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginHardeningSignoffState {
  version: string
  items: Record<string, IbGatewayPluginHardeningItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_hardening_signoff'

function emptyItemState(): IbGatewayPluginHardeningItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginHardeningSignoffState(): IbGatewayPluginHardeningSignoffState {
  const items: Record<string, IbGatewayPluginHardeningItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_HARDENING_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_HARDENING_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginHardeningSignoffState(): IbGatewayPluginHardeningSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginHardeningSignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginHardeningSignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_HARDENING_VERSION) {
      return defaultIbGatewayPluginHardeningSignoffState()
    }
    const merged = defaultIbGatewayPluginHardeningSignoffState()
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
    return defaultIbGatewayPluginHardeningSignoffState()
  }
}

export function saveIbGatewayPluginHardeningSignoffState(state: IbGatewayPluginHardeningSignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginHardeningItemsVerified(state: IbGatewayPluginHardeningSignoffState): boolean {
  return IB_GATEWAY_PLUGIN_HARDENING_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginHardeningVerificationCount(state: IbGatewayPluginHardeningSignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_HARDENING_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_HARDENING_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginHardeningSignedOff(): boolean {
  return loadIbGatewayPluginHardeningSignoffState().signedOffAt != null
}

export function priorIbGatewayPluginHardeningPrerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isIbGatewayPluginProgramSignedOff()) {
    missing.push('Program completion sign-off (IBGP-PC) required first')
  }
  return { ok: missing.length === 0, missing }
}
