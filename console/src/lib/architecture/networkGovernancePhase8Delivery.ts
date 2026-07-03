/** Network governance program — Phase 8 Program closure & implementation handoff delivery checklist. */

import {
  NETWORK_GOVERNANCE_PROGRAM_PHASES,
  isNetworkGovernancePhaseSignedOff,
} from './networkGovernanceProgramStatus'
import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const NETWORK_GOVERNANCE_PHASE8_VERSION = '2026-07-03'

export interface NetworkGovernancePhase8DeliveryItem {
  id: 'NG8-1' | 'NG8-2' | 'NG8-3' | 'NG8-4' | 'NG8-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const NETWORK_GOVERNANCE_PHASE8_DELIVERY_ITEMS: NetworkGovernancePhase8DeliveryItem[] = [
  {
    id: 'NG8-1',
    title: 'Network Governance program status strip',
    summary:
      'Blueprint shows NG1–NG7 sign-off progress strip — each phase tag shows ✓ when Owner has signed that phase panel.',
    verifySteps: [
      'Architecture → Blueprint — “Network Governance program” strip shows NG1–NG7 tags.',
      'Each tag shows ✓ when that phase is signed off in its panel below.',
      'When all seven signed, strip reads “All phases signed — ready for Phase 8 program closure”.',
    ],
  },
  {
    id: 'NG8-2',
    title: 'Three-layer doctrine delivered (Constitution / Spine / Projection)',
    summary:
      'Network governance catalogs span all three layers: Principle 8 + L0/L1/L2 (Constitution), D9 + network streams (Spine), Network Upgrade / Health / API contract (Projection).',
    verifySteps: [
      'Blueprint → Design principles — Principle 8 “Network is the ground floor”.',
      'Program / Briefing — D9 Session v2 and network-upgrade-core / network-upgrade-wifi streams visible.',
      'Architecture → Network Upgrade, Network API; Control Room → Network Health panel loads catalog projection.',
    ],
  },
  {
    id: 'NG8-3',
    title: 'Seven phase panels reachable on Blueprint',
    summary:
      'Owner can scroll Blueprint and reach every Network Governance Phase 1–7 sign-off panel (all SIGNED before Phase 8).',
    verifySteps: [
      'Blueprint — Network Governance Phase 1 through Phase 7 panels visible in order.',
      'Each prior panel shows SIGNED tag (historical sign-off ok).',
      'Phase 8 panel appears after Phase 7 with prior-phase gate message cleared.',
    ],
  },
  {
    id: 'NG8-4',
    title: 'Implementation handoff — unifi-mcp-server stream',
    summary:
      'Phase 8 documents next work outside governance-only delivery: spine stream unifi-mcp-server (REST client → MCP read → live probe → MCP write) and remaining network-upgrade-core / wifi rollout.',
    verifySteps: [
      'Program → Briefing — unifi-mcp-server stream 0/4 with prerequisites listed.',
      'Network API page — Future MCP tools table maps to planned /api/v1/network/* routes.',
      'Control Room Network Health — futureProbe references live probe after unifi-mcp-server ①–②.',
    ],
  },
  {
    id: 'NG8-5',
    title: 'Network Governance program closure sign-off',
    summary:
      'Owner accepts Constitution / Spine / Projection network governance program (NG1–NG7) as delivered — maintenance mode for network doctrine catalogs.',
    verifySteps: [
      'All NG1–NG7 phase panels show SIGNED before enabling Phase 8 sign-off.',
      'Sign off Phase 8 — panel shows NETWORK GOVERNANCE PROGRAM COMPLETE.',
      'Future live UniFi probe / platform-api routes are implementation-track work, not this program.',
    ],
  },
]

export interface NetworkGovernancePhase8ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface NetworkGovernancePhase8SignoffState {
  version: string
  items: Record<string, NetworkGovernancePhase8ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_network_governance_phase8_signoff'

function emptyItemState(): NetworkGovernancePhase8ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultNetworkGovernancePhase8SignoffState(): NetworkGovernancePhase8SignoffState {
  const items: Record<string, NetworkGovernancePhase8ItemVerification> = {}
  for (const item of NETWORK_GOVERNANCE_PHASE8_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: NETWORK_GOVERNANCE_PHASE8_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadNetworkGovernancePhase8SignoffState(): NetworkGovernancePhase8SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultNetworkGovernancePhase8SignoffState()
    const parsed = JSON.parse(raw) as NetworkGovernancePhase8SignoffState
    if (parsed.version !== NETWORK_GOVERNANCE_PHASE8_VERSION) {
      return defaultNetworkGovernancePhase8SignoffState()
    }
    const merged = defaultNetworkGovernancePhase8SignoffState()
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
    return defaultNetworkGovernancePhase8SignoffState()
  }
}

export function saveNetworkGovernancePhase8SignoffState(state: NetworkGovernancePhase8SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allNetworkGovernancePhase8ItemsVerified(state: NetworkGovernancePhase8SignoffState): boolean {
  return NETWORK_GOVERNANCE_PHASE8_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function networkGovernancePhase8VerificationCount(state: NetworkGovernancePhase8SignoffState): {
  verified: number
  total: number
} {
  const verified = NETWORK_GOVERNANCE_PHASE8_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: NETWORK_GOVERNANCE_PHASE8_DELIVERY_ITEMS.length }
}

export function isNetworkGovernancePhase8SignedOff(): boolean {
  return loadNetworkGovernancePhase8SignoffState().signedOffAt != null
}

export function priorNetworkGovernancePhasesSignedOff(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  for (const p of NETWORK_GOVERNANCE_PROGRAM_PHASES) {
    if (!isNetworkGovernancePhaseSignedOff(p.id)) {
      missing.push(`${p.id} ${p.shortLabel}`)
    }
  }
  return { ok: missing.length === 0, missing }
}
