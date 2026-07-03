/** Network governance program — Phase 3 Spine extension delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE3_VERSION = '2026-07-03'

export interface NetworkGovernancePhase3DeliveryItem {
  id: 'NG3-1' | 'NG3-2' | 'NG3-3' | 'NG3-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE3_DELIVERY_ITEMS: NetworkGovernancePhase3DeliveryItem[] = [
  {
    id: 'NG3-1',
    title: 'Spine decision D9 — Session v2 actuation path',
    summary:
      'ops-context.yaml records D9: Integration API Key blocked on UCG 10.4.57; bifrost-agent Session v2 + CSRF is primary firewall/zone write path.',
    verifySteps: [
      'Architecture → Milestones (Program) → Owner decisions — card D9 with topic “Network actuation path — Session v2 API primary”.',
      'Status SIGNED, signed_at 2026-07-02; conclusion mentions site UUID missing and Session v2 primary.',
      'Authority references scripts/unifi_firewall_setup.py and Blueprint Principle 8.',
    ],
  },
  {
    id: 'NG3-2',
    title: 'Stream network-upgrade-core — firewall step complete',
    summary:
      'infra track stream done advances to 5/6; note ⑥ documents Session v2 ZBF applied (5 zones + 9 policies) and Integration Key blocked.',
    verifySteps: [
      'Agent Briefing → Infra / network work lane — network-upgrade-core shows 5/6 (not 4/6).',
      'Stream note contains “⑥ Firewall ✓”, “Session v2”, “decision D9”, and “Integration API Key blocked”.',
      'Step ⑤ still marked pending (UniFi MCP read integration).',
    ],
  },
  {
    id: 'NG3-3',
    title: 'Coupling surface unifi_session_v2',
    summary:
      'Spine coupling_surfaces lists unifi_session_v2 alongside ib_client_id — documents Session auth path for UCG actuation.',
    verifySteps: [
      'Architecture → Milestones (Program) → Coupling surfaces — list includes unifi_session_v2.',
      'GET /api/v1/context returns coupling_surfaces containing unifi_session_v2.',
    ],
  },
  {
    id: 'NG3-4',
    title: 'Spine live snapshot reflects network governance',
    summary:
      'Blueprint Spine zone snapshot loads from GET /api/v1/context; decisions count includes D9 after platform-api reload.',
    verifySteps: [
      'Blueprint → Spine snapshot (live) — context loads without error after Refresh.',
      'Program page Overview still shows ops-context.yaml meta.version (spine file header).',
    ],
  },
]

export interface NetworkGovernancePhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase3SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase3_signoff'

function emptyItemState(): NetworkGovernancePhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase3SignoffState(): NetworkGovernancePhase3SignoffState {
  const items: Record<string, NetworkGovernancePhase3ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase3SignoffState(): NetworkGovernancePhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase3SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase3SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE3_VERSION) {
      return defaultNetworkGovernancePhase3SignoffState()
    }
    const merged = defaultNetworkGovernancePhase3SignoffState()
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
    return defaultNetworkGovernancePhase3SignoffState()
  }
}

export function saveNetworkGovernancePhase3SignoffState(state: NetworkGovernancePhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase3ItemsVerified(state: NetworkGovernancePhase3SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase3VerificationCount(state: NetworkGovernancePhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE3_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase3SignedOff(): boolean {
  return loadNetworkGovernancePhase3SignoffState().signedOffAt != null
}
