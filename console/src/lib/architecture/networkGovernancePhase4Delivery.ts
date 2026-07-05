/** Network governance program — Phase 4 Agent Protocol extension delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE4_VERSION = '2026-07-03'

export interface NetworkGovernancePhase4DeliveryItem {
  id: 'NG4-1' | 'NG4-2' | 'NG4-3' | 'NG4-4' | 'NG4-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE4_DELIVERY_ITEMS: NetworkGovernancePhase4DeliveryItem[] = [
  {
    id: 'NG4-1',
    title: 'Forbidden actions — network posture & actuation',
    summary:
      'FORBIDDEN_ACTIONS adds Default Security Posture / IDS/IPS, bulk zone delete, manual UniFi UI, and Integration Key write on UCG 10.4.57.',
    verifySteps: [
      'Governance → Agent Protocol → Forbidden actions — rows for Default Security Posture, bulk delete zones, manual UniFi UI.',
      'Integration API Key write row scoped to Ops mode with D9 / site UUID reference.',
      'Copy Prompt for LLM — Forbidden actions section includes network rows.',
    ],
  },
  {
    id: 'NG4-2',
    title: 'Ops mode — network L0/L1 agentMay + must-not',
    summary:
      'Ops mode Agent may includes firewall audit + L1 Session v2 apply; Agent must not includes Default Security Posture / IDS/IPS toggle.',
    verifySteps: [
      'Agent Protocol → Agent modes — Ops row mentions unifi_firewall_setup.py audit and D9 Session v2.',
      'Ops Agent must not mentions Default Security Posture or disable IDS/IPS.',
      'Mode selection hints include tracks.infra / network-upgrade-* → Ops.',
    ],
  },
  {
    id: 'NG4-3',
    title: 'Network diagnostic playbooks table',
    summary:
      'Four classifications (POLICY_NOMINAL, POLICY_DRIFT, SESSION_PATH, POSTURE_FORBIDDEN) with L0/L1/L2 autonomy — parallel to Mission verify_payload playbooks.',
    verifySteps: [
      'Agent Protocol → Network diagnostic playbooks (firewall / zone) — four rows rendered.',
      'POLICY_DRIFT mentions L1 apply idempotent + D9; SESSION_PATH mentions Integration Key blocked.',
      'POSTURE_FORBIDDEN cites Blueprint forbidden + AI Platform Network Security Posture.',
    ],
  },
  {
    id: 'NG4-4',
    title: 'LLM pack + opening prompt network alignment',
    summary:
      'buildAgentProtocolLlmPack includes Network diagnostic playbooks section; Ops opening prompt example references FIREWALL_RULES audit.',
    verifySteps: [
      'Agent Protocol → Copy Prompt for LLM — section “Network diagnostic playbooks” with audit script + four classifications.',
      'Example opening prompts — Ops example mentions firewall audit, L1 apply if drift, D9 Session v2.',
    ],
  },
  {
    id: 'NG4-5',
    title: 'Agent Protocol catalog version bump',
    summary: 'AGENT_PROTOCOL_VERSION advances to 2026-07-03; Blueprint Phase 4 sign-off panel mounted.',
    verifySteps: [
      'Agent Protocol Overview shows agentProtocolCatalog.ts v2026-07-03.',
      'Blueprint → Network Governance Phase 4 · Agent Protocol sign-off panel present (NG4-1..NG4-5).',
    ],
  },
]

export interface NetworkGovernancePhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase4SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase4_signoff'

function emptyItemState(): NetworkGovernancePhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase4SignoffState(): NetworkGovernancePhase4SignoffState {
  const items: Record<string, NetworkGovernancePhase4ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase4SignoffState(): NetworkGovernancePhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase4SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase4SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE4_VERSION) {
      return defaultNetworkGovernancePhase4SignoffState()
    }
    const merged = defaultNetworkGovernancePhase4SignoffState()
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
    return defaultNetworkGovernancePhase4SignoffState()
  }
}

export function saveNetworkGovernancePhase4SignoffState(state: NetworkGovernancePhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase4ItemsVerified(state: NetworkGovernancePhase4SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase4VerificationCount(state: NetworkGovernancePhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE4_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase4SignedOff(): boolean {
  return loadNetworkGovernancePhase4SignoffState().signedOffAt != null
}
