/** Network governance program — Phase 2 AI Platform capabilities extension delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE2_VERSION = '2026-07-03'

export interface NetworkGovernancePhase2DeliveryItem {
  id: 'NG2-1' | 'NG2-2' | 'NG2-3' | 'NG2-4' | 'NG2-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE2_DELIVERY_ITEMS: NetworkGovernancePhase2DeliveryItem[] = [
  {
    id: 'NG2-1',
    title: 'Discovery — UniFi network topology example',
    summary:
      'AI Platform capabilities → Discovery adds UniFi inventory, zone-matrix, and per-VLAN client count as an Agent/human-readable probe target.',
    verifySteps: [
      'Blueprint → AI Platform capabilities → Discovery — fourth bullet mentions UniFi API, UCG/Switch/AP, zone-matrix.',
      'Copy Prompt for LLM — Discovery examples include network topology bullet.',
    ],
  },
  {
    id: 'NG2-2',
    title: 'Maintenance — firewall policy drift detection',
    summary:
      'Maintenance adds firewall drift audit against networkUpgradeCatalog.ts FIREWALL_RULES — parallel to ArgoCD config drift.',
    verifySteps: [
      'Blueprint → AI Platform capabilities → Maintenance — bullet mentions firewall policy drift and FIREWALL_RULES.',
      'Text references networkUpgradeCatalog.ts as authoritative catalog.',
    ],
  },
  {
    id: 'NG2-3',
    title: 'Repair — L1/L2 network actuation examples',
    summary:
      'Repair adds L1 idempotent firewall apply (unifi_firewall_setup.py) and L2 zone/SSID changes requiring Owner confirm.',
    verifySteps: [
      'Blueprint → AI Platform capabilities → Repair — bullet for L1 network firewall apply (idempotent).',
      'Repair — bullet for L2 network zone restructure / SSID CRUD with Owner confirmation.',
    ],
  },
  {
    id: 'NG2-4',
    title: 'AI Platform success — Network area',
    summary:
      'AI Platform success criteria include Network: zone-matrix + policy list + AP health via platform-api; drift auto-detected.',
    verifySteps: [
      'Blueprint → AI Platform success criteria — Area “Network” row present.',
      'Criterion mentions zone-matrix, firewall policy list, AP health, and firewall drift.',
    ],
  },
  {
    id: 'NG2-5',
    title: 'AI Platform boundaries — Network Security Posture',
    summary:
      'Boundaries table adds rule forbidding Agent from toggling Default Security Posture or disabling IDS/IPS; UCG physical access Owner-only.',
    verifySteps: [
      'Blueprint → AI Platform boundaries — Rule “Network Security Posture” row present.',
      'Detail mentions Default Security Posture, IDS/IPS, and Owner-only physical UCG access.',
    ],
  },
]

export interface NetworkGovernancePhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase2SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase2_signoff'

function emptyItemState(): NetworkGovernancePhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase2SignoffState(): NetworkGovernancePhase2SignoffState {
  const items: Record<string, NetworkGovernancePhase2ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase2SignoffState(): NetworkGovernancePhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase2SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase2SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE2_VERSION) {
      return defaultNetworkGovernancePhase2SignoffState()
    }
    const merged = defaultNetworkGovernancePhase2SignoffState()
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
    return defaultNetworkGovernancePhase2SignoffState()
  }
}

export function saveNetworkGovernancePhase2SignoffState(state: NetworkGovernancePhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase2ItemsVerified(state: NetworkGovernancePhase2SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase2VerificationCount(state: NetworkGovernancePhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE2_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase2SignedOff(): boolean {
  return loadNetworkGovernancePhase2SignoffState().signedOffAt != null
}
