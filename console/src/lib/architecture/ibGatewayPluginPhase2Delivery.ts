/** IB Gateway Plugin — Phase 2 delivery checklist (Platform API + Console). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isIbGatewayPluginPhase1SignedOff } from './ibGatewayPluginPhase1Delivery'

export const IB_GATEWAY_PLUGIN_PHASE2_VERSION = '2026-07-04'

export interface IbGatewayPluginPhase2DeliveryItem {
  id: 'IBGP2-1' | 'IBGP2-2' | 'IBGP2-3' | 'IBGP2-4' | 'IBGP2-5' | 'IBGP2-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS: IbGatewayPluginPhase2DeliveryItem[] = [
  {
    id: 'IBGP2-1',
    title: 'platform-api GET /plugins/ib-gateway/status',
    summary: 'L0 probe — K8s ib-gateway deployment + redis-ib legacy health keys + slot ib:health:*.',
    verifySteps: [
      'curl http://127.0.0.1:8780/api/v1/plugins/ib-gateway/status → reachable, mode, summary.',
      'Response includes deployment.ready, ingestor_health.connected, slots wzhao1503/vzhao1503.',
      'Requires REDIS_IB_PLATFORM_PASS in bifrost-platform/.env.',
    ],
  },
  {
    id: 'IBGP2-2',
    title: 'POST control/reconnect (L1 operator)',
    summary: 'Rollout restart data/ib-gateway via existing cluster actuation path.',
    verifySteps: [
      'POST /api/v1/plugins/ib-gateway/control/reconnect with operator token.',
      'Returns ok:true, action ib-gateway.reconnect, audit logged.',
      'Deployment restarts; status probe recovers within ~30s.',
    ],
  },
  {
    id: 'IBGP2-3',
    title: 'POST control/maintenance (L1 operator)',
    summary: 'SET ib:control:{account_id} on redis-ib — platform ACL user writes maintenance flag.',
    verifySteps: [
      'POST .../control/maintenance body {"account_id":"wzhao1503","enabled":true}.',
      'redis GET ib:control:wzhao1503 shows maintenance JSON.',
      'POST with enabled:false clears flag.',
    ],
  },
  {
    id: 'IBGP2-4',
    title: 'useIbGatewayLiveProbe hook',
    summary: 'TanStack Query polls status every 30s — maps reachability to StatusLamp.',
    verifySteps: [
      'console/src/hooks/useIbGatewayLiveProbe.ts — probeReach ok/degraded/fail/unknown.',
      'Hook used by IbGatewayLiveStatusPanel on Subcontractors → Plugin Gallery.',
    ],
  },
  {
    id: 'IBGP2-5',
    title: 'Console live status + control panel',
    summary: 'IbGatewayLiveStatusPanel — StatusLamp, slot table, reconnect + maintenance actions.',
    verifySteps: [
      'Subcontractors → Plugin Gallery — IB Gateway live status section (platform-api probe).',
      'Operator token enables Reconnect + per-slot maintenance Enter/Clear.',
      'Summary shows mock/live mode and deployment ready fraction.',
    ],
  },
  {
    id: 'IBGP2-6',
    title: 'Console Phase 2 sign-off panel',
    summary: 'IBGP2 checklist + Owner sign-off after API + UI verified.',
    verifySteps: [
      'Phase 1 signed off; Phase 2 panel visible.',
      'Mark all IBGP2 items verified → Sign off Phase 2 (Admin token).',
      'Program strip shows IBGP2 ✓ — proceed to Phase 3 Trade cutover.',
    ],
  },
]

export interface IbGatewayPluginPhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginPhase2SignoffState {
  version: string
  items: Record<string, IbGatewayPluginPhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_phase2_signoff'

function emptyItemState(): IbGatewayPluginPhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginPhase2SignoffState(): IbGatewayPluginPhase2SignoffState {
  const items: Record<string, IbGatewayPluginPhase2ItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginPhase2SignoffState(): IbGatewayPluginPhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginPhase2SignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginPhase2SignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_PHASE2_VERSION) {
      return defaultIbGatewayPluginPhase2SignoffState()
    }
    const merged = defaultIbGatewayPluginPhase2SignoffState()
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
    return defaultIbGatewayPluginPhase2SignoffState()
  }
}

export function saveIbGatewayPluginPhase2SignoffState(state: IbGatewayPluginPhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginPhase2ItemsVerified(state: IbGatewayPluginPhase2SignoffState): boolean {
  return IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginPhase2VerificationCount(state: IbGatewayPluginPhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_PHASE2_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginPhase2SignedOff(): boolean {
  return loadIbGatewayPluginPhase2SignoffState().signedOffAt != null
}

export function priorIbGatewayPluginPhase2Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isIbGatewayPluginPhase1SignedOff()) {
    missing.push('IBGP1 Phase 1 sign-off required')
  }
  return { ok: missing.length === 0, missing }
}
