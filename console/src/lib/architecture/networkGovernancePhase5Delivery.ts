/** Network governance program — Phase 5 Network Upgrade catalog delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE5_VERSION = '2026-07-03'

export interface NetworkGovernancePhase5DeliveryItem {
  id: 'NG5-1' | 'NG5-2' | 'NG5-3' | 'NG5-4' | 'NG5-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE5_DELIVERY_ITEMS: NetworkGovernancePhase5DeliveryItem[] = [
  {
    id: 'NG5-1',
    title: 'Catalog version + status — partial deploy',
    summary:
      'networkUpgradeCatalog.ts advances to 2026-07-03-v2; NET_UPGRADE_STATUS reflects UCG + switch live, firewall applied, AP rollout pending (not RESEARCH/ORDERED).',
    verifySteps: [
      'Architecture → Network Upgrade → Catalog metadata — Version 2026-07-03-v2.',
      'Status tag shows PARTIAL DEPLOY with VLAN 10/20/30/50 and Session v2 firewall note.',
      'Copy for LLM — header Status matches UI (not 2026-06-26-v1 / ORDERED).',
    ],
  },
  {
    id: 'NG5-2',
    title: 'FIREWALL_APPLIED — ZBF authority block',
    summary:
      'FIREWALL_APPLIED documents 5 Bifrost zones + 9 policies, Session v2 path (D9), audit script, and mapping to FIREWALL_RULES.',
    verifySteps: [
      'Network Upgrade → Firewall applied (ZBF — Session v2) section — 9 policy rows with catalogRule mapping.',
      'Section mentions scripts/unifi_firewall_setup.py audit and spine D9.',
      'FIREWALL_APPLIED.policyCount === 9 and zoneCount === 5 in catalog source.',
    ],
  },
  {
    id: 'NG5-3',
    title: 'Research items — firewall + Session v2 answered',
    summary:
      'Research table adds firewall-applied and session-v2-actuation (answered); moca-vlan blocked pending physical test script.',
    verifySteps: [
      'Network Upgrade → Research items — row firewall-applied status answered with FIREWALL_APPLIED reference.',
      'Row session-v2-actuation answered with D9 + unifi_firewall_setup.py authority.',
      'moca-vlan status blocked (not open) with unifi_moca_vlan_test.sh note.',
    ],
  },
  {
    id: 'NG5-4',
    title: 'Deployment progress + hardware BOM alignment',
    summary:
      'DEPLOYMENT_PROGRESS mirrors spine network-upgrade-core 5/6 and network-upgrade-wifi 2/5; UCG + switch BOM status owned.',
    verifySteps: [
      'Network Upgrade → Deployment progress — network-upgrade-core 5/6 with firewall ✓ note.',
      'network-upgrade-wifi 2/5 with ① AP purchase + ③ pre-AP tooling done; ② WiFiman survey pending.',
      'Hardware BOM — UCG Max and USW-Pro-Max-24 status owned (not ordered).',
    ],
  },
  {
    id: 'NG5-5',
    title: 'LLM pack + Blueprint Phase 5 sign-off panel',
    summary:
      'buildNetworkUpgradeLlmPack includes deployment progress + firewall applied sections; Blueprint mounts Network Governance Phase 5 panel.',
    verifySteps: [
      'Copy for LLM — sections “Deployment progress” and “Firewall applied (ZBF — Session v2)”.',
      'Blueprint → Network Governance Phase 5 · Network Upgrade catalog sign-off panel (NG5-1..NG5-5).',
      'Prior Network Phases 1–4 show SIGNED before signing Phase 5.',
    ],
  },
]

export interface NetworkGovernancePhase5ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase5SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase5ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase5_signoff'

function emptyItemState(): NetworkGovernancePhase5ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase5SignoffState(): NetworkGovernancePhase5SignoffState {
  const items: Record<string, NetworkGovernancePhase5ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE5_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE5_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase5SignoffState(): NetworkGovernancePhase5SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase5SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase5SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE5_VERSION) {
      return defaultNetworkGovernancePhase5SignoffState()
    }
    const merged = defaultNetworkGovernancePhase5SignoffState()
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
    return defaultNetworkGovernancePhase5SignoffState()
  }
}

export function saveNetworkGovernancePhase5SignoffState(state: NetworkGovernancePhase5SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase5ItemsVerified(state: NetworkGovernancePhase5SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE5_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase5VerificationCount(state: NetworkGovernancePhase5SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE5_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE5_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase5SignedOff(): boolean {
  return loadNetworkGovernancePhase5SignoffState().signedOffAt != null
}
