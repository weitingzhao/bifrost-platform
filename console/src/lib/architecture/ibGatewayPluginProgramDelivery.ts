/** IB Gateway Plugin — Program completion delivery (after IBGP0–4 phase sign-offs). */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'
import { allIbGatewayPluginPhasesSignedOff } from './ibGatewayPluginProgramStatus'

export const IB_GATEWAY_PLUGIN_PROGRAM_VERSION = '2026-07-04'

export interface IbGatewayPluginProgramDeliveryItem {
  id: 'IBGP-PC-1' | 'IBGP-PC-2' | 'IBGP-PC-3' | 'IBGP-PC-4' | 'IBGP-PC-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const IB_GATEWAY_PLUGIN_PROGRAM_DELIVERY_ITEMS: IbGatewayPluginProgramDeliveryItem[] = [
  {
    id: 'IBGP-PC-1',
    title: 'verify-ib-gateway-program.sh',
    summary: 'Aggregate script — cutover + live TWS + status API program gates.',
    verifySteps: [
      'make verify-ib-gateway-program in bifrost-platform-plugin.',
      'Runs verify-trade-cutover + verify-ib-gateway-live + status assertions.',
      'All three steps must pass before program sign-off.',
    ],
  },
  {
    id: 'IBGP-PC-2',
    title: 'Phase sign-offs IBGP0–4',
    summary: 'All five phase panels signed — direct replacement, no parallel window.',
    verifySteps: [
      'Program strip shows IBGP0 ✓ … IBGP4 ✓.',
      'Each phase panel status SIGNED with timestamp.',
      'Phase 4 completed live TWS cutover.',
    ],
  },
  {
    id: 'IBGP-PC-3',
    title: 'Live Platform IB bus operational',
    summary: 'data/ib-gateway @ live mode; redis-ib shared bus; Trade reads ExternalName.',
    verifySteps: [
      'GET status: mode=live, host + secondary connected.',
      'ingestor/account/operator health mode=live.',
      'cutover.legacy_socket_retired=true all Trade NS.',
    ],
  },
  {
    id: 'IBGP-PC-4',
    title: 'Catalog + implementation phases done',
    summary: 'ibGatewayPluginCatalog — IBGP0–4 status done; five-phase program closed.',
    verifySteps: [
      'Implementation phases table — all five rows status done.',
      'Design principles unchanged; redis-ib contract stable.',
      'Legacy trade-socket IB retired (replicas=0).',
    ],
  },
  {
    id: 'IBGP-PC-5',
    title: 'Program completion sign-off panel',
    summary: 'Owner confirms IB Gateway Plugin program complete — Platform TWS bus in production.',
    verifySteps: [
      'This panel visible after IBGP4 signed off.',
      'Mark all IBGP-PC items verified → Sign off program (Admin token).',
      'Strip shows IB GATEWAY PLUGIN COMPLETE.',
    ],
  },
]

export interface IbGatewayPluginProgramItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface IbGatewayPluginProgramSignoffState {
  version: string
  items: Record<string, IbGatewayPluginProgramItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_ib_gateway_plugin_program_signoff'

function emptyItemState(): IbGatewayPluginProgramItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultIbGatewayPluginProgramSignoffState(): IbGatewayPluginProgramSignoffState {
  const items: Record<string, IbGatewayPluginProgramItemVerification> = {}
  for (const item of IB_GATEWAY_PLUGIN_PROGRAM_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: IB_GATEWAY_PLUGIN_PROGRAM_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadIbGatewayPluginProgramSignoffState(): IbGatewayPluginProgramSignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultIbGatewayPluginProgramSignoffState()
    const parsed = JSON.parse(raw) as IbGatewayPluginProgramSignoffState
    if (parsed.version !== IB_GATEWAY_PLUGIN_PROGRAM_VERSION) {
      return defaultIbGatewayPluginProgramSignoffState()
    }
    const merged = defaultIbGatewayPluginProgramSignoffState()
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
    return defaultIbGatewayPluginProgramSignoffState()
  }
}

export function saveIbGatewayPluginProgramSignoffState(state: IbGatewayPluginProgramSignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allIbGatewayPluginProgramItemsVerified(state: IbGatewayPluginProgramSignoffState): boolean {
  return IB_GATEWAY_PLUGIN_PROGRAM_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function ibGatewayPluginProgramVerificationCount(state: IbGatewayPluginProgramSignoffState): {
  verified: number
  total: number
} {
  const verified = IB_GATEWAY_PLUGIN_PROGRAM_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: IB_GATEWAY_PLUGIN_PROGRAM_DELIVERY_ITEMS.length }
}

export function isIbGatewayPluginProgramSignedOff(): boolean {
  return loadIbGatewayPluginProgramSignoffState().signedOffAt != null
}

export function priorIbGatewayPluginProgramPrerequisites(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (!allIbGatewayPluginPhasesSignedOff()) {
    missing.push('All phase sign-offs IBGP0–IBGP4 required')
  }
  return { ok: missing.length === 0, missing }
}
