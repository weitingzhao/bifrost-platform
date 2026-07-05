/** IB Gateway Plugin — Phase 4 delivery checklist (Live TWS cutover). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { isIbGatewayPluginPhase3SignedOff } from './ibGatewayPluginPhase3Delivery'

export const IB_GATEWAY_PLUGIN_PHASE4_VERSION = '2026-07-04'

export interface IbGatewayPluginPhase4DeliveryItem {
  id: 'IBGP4-1' | 'IBGP4-2' | 'IBGP4-3' | 'IBGP4-4' | 'IBGP4-5' | 'IBGP4-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_PHASE4_DELIVERY_ITEMS: IbGatewayPluginPhase4DeliveryItem[] = [
  {
    id: 'IBGP4-1',
    title: 'POST control/mode (L1 operator)',
    summary: 'Switch ib-gateway mock ↔ live — patches ConfigMap + rollout restart.',
    verifySteps: [
      'POST /api/v1/plugins/ib-gateway/control/mode body {"mode":"live"} with operator token.',
      'Returns ok:true, action ib-gateway.mode, audit logged.',
      'POST {"mode":"mock"} reverts for maintenance windows.',
    ],
  },
  {
    id: 'IBGP4-2',
    title: 'ConfigMap mode + gateway.yaml sync',
    summary: 'data/ib-gateway-config — top-level mode key + gateway.yaml mode line stay in sync.',
    verifySteps: [
      'kubectl get cm ib-gateway-config -n data -o jsonpath="{.data.mode}".',
      'gateway.yaml first line mode matches; IB_GATEWAY_MODE env from ConfigMap.',
      'Rollout completes; pod logs show live or mock startup.',
    ],
  },
  {
    id: 'IBGP4-3',
    title: 'verify-ib-gateway-live.sh',
    summary: 'Cluster script — mode=live, deployment ready, status slots connected, operator ping.',
    verifySteps: [
      'make verify-ib-gateway-live (after switching to live).',
      'Requires TWS reachable @ 192.168.10.30 / .32:7496.',
      'Off-hours bid/ask -1 on ticks is normal; slots must show connected.',
    ],
  },
  {
    id: 'IBGP4-4',
    title: 'Live dual-TWS slot probe',
    summary: 'GET status — mode live, wzhao1503 + vzhao1503 slots connected via real TWS sockets.',
    verifySteps: [
      'status.mode === live; ingestor_health.mode === live.',
      'slots host/secondary connected=true; reachability ok or degraded (market hours).',
      'ib:account:snapshot:v1 populated when account agent pipeline runs.',
    ],
  },
  {
    id: 'IBGP4-5',
    title: 'Console mode switch UI',
    summary: 'IbGatewayLiveStatusPanel — Switch to live / Revert to mock with ConfirmDialog.',
    verifySteps: [
      'Rocket → Cluster — mode DenseTag shows MOCK or LIVE.',
      'Operator token enables mode switch buttons next to Reconnect.',
      'After switch, live probe refreshes within ~30s.',
    ],
  },
  {
    id: 'IBGP4-6',
    title: 'Console Phase 4 sign-off panel',
    summary: 'IBGP4 checklist + Owner sign-off — completes IB Gateway Plugin program.',
    verifySteps: [
      'Phase 3 signed off; Phase 4 panel visible.',
      'Mark all IBGP4 items verified → Sign off Phase 4 (Admin token).',
      'Program strip 5/5 ✓ — Platform IB bus fully operational on live TWS.',
    ],
  },
]

export interface IbGatewayPluginPhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginPhase4SignoffState {
  version: string
  items: Record<string, IbGatewayPluginPhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_phase4_signoff'

function emptyItemState(): IbGatewayPluginPhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginPhase4SignoffState(): IbGatewayPluginPhase4SignoffState {
  const items: Record<string, IbGatewayPluginPhase4ItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginPhase4SignoffState(): IbGatewayPluginPhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginPhase4SignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginPhase4SignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_PHASE4_VERSION) {
      return defaultIbGatewayPluginPhase4SignoffState()
    }
    const merged = defaultIbGatewayPluginPhase4SignoffState()
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
    return defaultIbGatewayPluginPhase4SignoffState()
  }
}

export function saveIbGatewayPluginPhase4SignoffState(state: IbGatewayPluginPhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginPhase4ItemsVerified(state: IbGatewayPluginPhase4SignoffState): boolean {
  return IB_GATEWAY_PLUGIN_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginPhase4VerificationCount(state: IbGatewayPluginPhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_PHASE4_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginPhase4SignedOff(): boolean {
  return loadIbGatewayPluginPhase4SignoffState().signedOffAt != null
}

export function priorIbGatewayPluginPhase4Prerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!isIbGatewayPluginPhase3SignedOff()) {
    missing.push('IBGP3 Phase 3 sign-off required')
  }
  return { ok: missing.length === 0, missing }
}
