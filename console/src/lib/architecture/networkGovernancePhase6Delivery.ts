/** Network governance program — Phase 6 Control Room views delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE6_VERSION = '2026-07-03'

export interface NetworkGovernancePhase6DeliveryItem {
  id: 'NG6-1' | 'NG6-2' | 'NG6-3' | 'NG6-4' | 'NG6-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE6_DELIVERY_ITEMS: NetworkGovernancePhase6DeliveryItem[] = [
  {
    id: 'NG6-1',
    title: 'Control Room — Network Health panel (diagnosis zone)',
    summary:
      'Ground Systems → Network — full UniFi panel; Mission Control → Control Room embeds Network Health summary.',
    verifySteps: [
      'Ground Systems → Network — section “Network Health — ground floor (LAN / UniFi)”. Mission Control → Control Room retains summary + cross-link.',
      'Stream table shows network-upgrade-core and network-upgrade-wifi with done/total progress.',
      'Catalog version 2026-07-03-v2 visible in panel header.',
    ],
  },
  {
    id: 'NG6-2',
    title: 'Firewall applied block + Agent playbook link',
    summary:
      'Panel documents ZBF apply (5 zones · 9 policies · D9 Session v2) and links POLICY_NOMINAL / POLICY_DRIFT to Agent Protocol.',
    verifySteps: [
      'Firewall sub-block mentions applied date, audit script unifi_firewall_setup.py audit, and FIREWALL_RULES drift target.',
      'Click “POLICY_NOMINAL / POLICY_DRIFT” or Agent Protocol button — navigates to Agent Protocol page.',
      'Future probe note references GET /api/v1/network/* (not implemented in this phase).',
    ],
  },
  {
    id: 'NG6-3',
    title: 'Spine live projection (source column)',
    summary:
      'When GET /api/v1/context loads, stream rows prefer spine done/total/notes; Source column shows spine vs catalog fallback.',
    verifySteps: [
      'Control Room loaded with platform-api — stream Source tags show spine (not catalog-only).',
      'network-upgrade-core progress matches Agent Briefing infra lane / ops-context.yaml (5/6).',
      'network-upgrade-wifi progress matches spine (2/5).',
    ],
  },
  {
    id: 'NG6-4',
    title: 'Control Room — self-contained network OS view',
    summary:
      'Network Health panel shows spine streams, live UniFi probe, and ZBF summary without Architecture page navigation.',
    verifySteps: [
      'Control Room Network Health — stream table + live probe + ZBF block (no Architecture deep links).',
      'Agent Protocol button still navigates for POLICY_DRIFT remediation.',
      'Catalog authority via Governance Copy All → networkUpgradeCatalog.ts historical appendix.',
    ],
  },
  {
    id: 'NG6-5',
    title: 'Blueprint CONSOLE_VIEWS + Phase 6 sign-off panel',
    summary:
      'blueprintCatalog CONSOLE_VIEWS adds Network Health (Control Room) entry; Blueprint mounts Network Governance Phase 6 panel.',
    verifySteps: [
      'Governance → Blueprint → Console views — row “Network Health (Control Room)” [Mission Control].',
      'Blueprint → Network Governance Phase 6 · Control Room views sign-off panel (NG6-1..NG6-5).',
      'Network Governance Phases 1–5 show SIGNED before signing Phase 6.',
    ],
  },
]

export interface NetworkGovernancePhase6ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase6SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase6ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase6_signoff'

function emptyItemState(): NetworkGovernancePhase6ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase6SignoffState(): NetworkGovernancePhase6SignoffState {
  const items: Record<string, NetworkGovernancePhase6ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE6_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE6_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase6SignoffState(): NetworkGovernancePhase6SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase6SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase6SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE6_VERSION) {
      return defaultNetworkGovernancePhase6SignoffState()
    }
    const merged = defaultNetworkGovernancePhase6SignoffState()
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
    return defaultNetworkGovernancePhase6SignoffState()
  }
}

export function saveNetworkGovernancePhase6SignoffState(state: NetworkGovernancePhase6SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase6ItemsVerified(state: NetworkGovernancePhase6SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE6_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase6VerificationCount(state: NetworkGovernancePhase6SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE6_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE6_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase6SignedOff(): boolean {
  return loadNetworkGovernancePhase6SignoffState().signedOffAt != null
}
