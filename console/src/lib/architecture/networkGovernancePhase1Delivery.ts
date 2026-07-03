/** Network governance program — Phase 1 Constitution extension delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE1_VERSION = '2026-07-02'

export interface NetworkGovernancePhase1DeliveryItem {
  id: 'NG1-1' | 'NG1-2' | 'NG1-3' | 'NG1-4' | 'NG1-5' | 'NG1-6'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE1_DELIVERY_ITEMS: NetworkGovernancePhase1DeliveryItem[] = [
  {
    id: 'NG1-1',
    title: 'Design Principle 8 — Network is the ground floor',
    summary:
      'Blueprint Constitution adds Principle 8: network infrastructure is fate-isolated substrate; UCG probe/actuation works independently of K8s.',
    verifySteps: [
      'Architecture → Blueprint → Design principles — row 8 title is “Network is the ground floor”.',
      'Description mentions UCG / Switch / AP and Session-connect bypassing cluster.',
      'Copy Prompt for LLM includes “8. **Network is the ground floor**”.',
    ],
  },
  {
    id: 'NG1-2',
    title: 'Authorization levels — network L0/L1/L2/forbidden semantics',
    summary:
      'L0 adds zone-matrix audit + AP status; L1 adds idempotent firewall apply; L2 adds zone/SSID/posture; forbidden adds bulk zone wipe + IDS/IPS disable.',
    verifySteps: [
      'Authorization levels table — L0 behavior mentions zone-matrix and VLAN binding.',
      'L1 mentions firewall policy apply (idempotent Bifrost rules).',
      'forbidden row mentions bulk delete Bifrost zones and disable IDS/IPS.',
    ],
  },
  {
    id: 'NG1-3',
    title: 'Strategy C — UniFi Controller as mature component',
    summary:
      'Mature components layer wraps UniFi Controller via Session v2 API (future Integration), same pattern as Argo/Tekton.',
    verifySteps: [
      'Strategy C table — “Mature components” responsibility lists UniFi Controller.',
      'Text says wrapped via API, not replacing control plane.',
    ],
  },
  {
    id: 'NG1-4',
    title: 'Owner exceptions — physical vs manual UniFi UI',
    summary:
      'Owner may physically swap UCG/Switch/AP; routine firewall/zone/SSID changes must go through platform-api + scripts, not UniFi UI.',
    verifySteps: [
      'Owner exceptions table — fourth row: allowed = physical hardware swap.',
      'Forbidden column says manual UniFi UI firewall/zone/SSID changes.',
    ],
  },
  {
    id: 'NG1-5',
    title: 'Success criteria — Network North Star completion',
    summary:
      'Constitution success criteria include Network area: zone-policy audit, AP baseline, Default VLAN empty — via /api/v1/network/*.',
    verifySteps: [
      'Success criteria table — Area column includes “Network”.',
      'Criterion mentions zone-policy audit and /api/v1/network/*.',
    ],
  },
  {
    id: 'NG1-6',
    title: 'Actuation phases — network overlay on P0/P1/P2/P5',
    summary:
      'P0 adds UCG probe + zone-matrix; P1 firewall audit + AP probe; P2 AP lifecycle; P5 UniFi MCP tools — without new phase IDs.',
    verifySteps: [
      'Actuation phases table — P0 deliverables mention UCG reachability and zone-matrix.',
      'P1 mentions firewall audit; P2 mentions AP lifecycle; P5 mentions UniFi MCP tools.',
      'P3 and P4 rows unchanged (no network overlay).',
    ],
  },
]

export interface NetworkGovernancePhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase1SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase1_signoff'

function emptyItemState(): NetworkGovernancePhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase1SignoffState(): NetworkGovernancePhase1SignoffState {
  const items: Record<string, NetworkGovernancePhase1ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase1SignoffState(): NetworkGovernancePhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase1SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase1SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE1_VERSION) {
      return defaultNetworkGovernancePhase1SignoffState()
    }
    const merged = defaultNetworkGovernancePhase1SignoffState()
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
    return defaultNetworkGovernancePhase1SignoffState()
  }
}

export function saveNetworkGovernancePhase1SignoffState(state: NetworkGovernancePhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase1ItemsVerified(state: NetworkGovernancePhase1SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase1VerificationCount(state: NetworkGovernancePhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE1_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase1SignedOff(): boolean {
  return loadNetworkGovernancePhase1SignoffState().signedOffAt != null
}
