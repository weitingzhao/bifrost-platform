/** Network governance program — Phase 7 platform-api /api/v1/network/* contract planning delivery. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE7_VERSION = '2026-07-03'

export interface NetworkGovernancePhase7DeliveryItem {
  id: 'NG7-1' | 'NG7-2' | 'NG7-3' | 'NG7-4' | 'NG7-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE7_DELIVERY_ITEMS: NetworkGovernancePhase7DeliveryItem[] = [
  {
    id: 'NG7-1',
    title: 'Network API contract catalog',
    summary:
      'networkApiContractCatalog.ts documents planned GET/POST /api/v1/network/* routes, L0/L1/L2 autonomy, auth levels, executors, and forbidden actions — all status planned (no Go handlers).',
    verifySteps: [
      'Architecture → Network API — metadata shows version 2026-07-03 and PLANNED status tag.',
      'Planned routes table lists status, zones, policies, audit, devices, clients, firewall/apply, zones/restructure, wlan.',
      'Forbidden section lists Default Security Posture toggle, bulk delete, Integration Key write, manual UI.',
    ],
  },
  {
    id: 'NG7-2',
    title: 'Architecture page — Network API',
    summary:
      'NetworkApiContractPage renders executor model, route table, future MCP tools, and Copy for LLM pack.',
    verifySteps: [
      'Sidebar K3s → Network API navigates to #network-api.',
      'Page header describes platform-api contract planning (Projection layer).',
      'Copy for LLM produces pack with routes, forbidden list, and MCP tool mapping.',
    ],
  },
  {
    id: 'NG7-3',
    title: 'Cross-refs — Control Room + Blueprint CONSOLE_VIEWS',
    summary:
      'Network Health panel links to Network API contract; blueprintCatalog CONSOLE_VIEWS adds Network API row.',
    verifySteps: [
      'Control Room → Network Health — “Network API contract” button opens Network API tab.',
      'futureProbe references networkApiContractCatalog.ts (not generic planned text only).',
      'Blueprint → Console views — row “Network API” [Architecture].',
    ],
  },
  {
    id: 'NG7-4',
    title: 'MCP tool mapping (unifi-mcp-server stream ⑤)',
    summary:
      'Future MCP read/routine/confirm tools map 1:1 to platform-api routes — decoupling per Constitution.',
    verifySteps: [
      'Network API page → Future MCP tools table lists get_network_status, audit_network_firewall, apply_network_firewall, etc.',
      'Each tool row shows corresponding /api/v1/network/* route and planned status.',
      'Executor model notes spine stream unifi-mcp-server wraps GET routes when implemented.',
    ],
  },
  {
    id: 'NG7-5',
    title: 'Blueprint Phase 7 sign-off panel',
    summary:
      'Blueprint mounts Network Governance Phase 7 sign-off panel (NG7-1..NG7-5); Phases 1–6 show SIGNED before signing Phase 7.',
    verifySteps: [
      'Architecture → Blueprint → Network Governance Phase 7 · Network API contract sign-off panel.',
      'Mark all NG7 items verified → Sign off Network Phase 7 delivery (Admin token).',
      'Sign-off confirms contract planning only — live platform-api routes remain future work.',
    ],
  },
]

export interface NetworkGovernancePhase7ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase7SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase7ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase7_signoff'

function emptyItemState(): NetworkGovernancePhase7ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase7SignoffState(): NetworkGovernancePhase7SignoffState {
  const items: Record<string, NetworkGovernancePhase7ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE7_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE7_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase7SignoffState(): NetworkGovernancePhase7SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase7SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase7SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE7_VERSION) {
      return defaultNetworkGovernancePhase7SignoffState()
    }
    const merged = defaultNetworkGovernancePhase7SignoffState()
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
    return defaultNetworkGovernancePhase7SignoffState()
  }
}

export function saveNetworkGovernancePhase7SignoffState(state: NetworkGovernancePhase7SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase7ItemsVerified(state: NetworkGovernancePhase7SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE7_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase7VerificationCount(state: NetworkGovernancePhase7SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE7_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE7_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase7SignedOff(): boolean {
  return loadNetworkGovernancePhase7SignoffState().signedOffAt != null
}
