/** Governance boundary program — Phase 5 Blueprint three-zone layout delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const GOVERNANCE_PHASE5_VERSION = '2026-07-01'

export interface GovernancePhase5DeliveryItem {
  id: 'GP5-1' | 'GP5-2' | 'GP5-3' | 'GP5-4' | 'GP5-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE5_DELIVERY_ITEMS: GovernancePhase5DeliveryItem[] = [
  {
    id: 'GP5-1',
    title: 'Three bordered governance zones on Blueprint',
    summary:
      'Architecture → Blueprint renders Constitution, Spine, and Projection as distinct visual regions with zone headers (not inline LayerBanner only).',
    verifySteps: [
      'Scroll Blueprint — three bordered zones appear in order: Constitution → Spine → Projection.',
      'Each zone header shows layer tag, change rate, and authority source.',
      'Constitution content (North Star, principles, AI Platform) sits inside Constitution zone only.',
    ],
  },
  {
    id: 'GP5-2',
    title: 'Spine zone owns semantics + live snapshot',
    summary:
      'Spine milestone semantics table and live GET /api/v1/context snapshot live in the Spine zone — not mixed into Constitution or Projection.',
    verifySteps: [
      'Spine zone contains “Spine milestone semantics” definition table.',
      'Spine zone contains “Spine snapshot (live)” when context loads (phase, active_track, focus).',
      'Projection zone does not duplicate spine snapshot block.',
    ],
  },
  {
    id: 'GP5-3',
    title: 'Zone jump navigation anchors',
    summary:
      'Jump to zone nav links scroll to #blueprint-zone-constitution, #blueprint-zone-spine, #blueprint-zone-projection.',
    verifySteps: [
      '“Jump to zone” bar appears above the three regions.',
      'Click Constitution / Spine / Projection — page scrolls to matching bordered zone.',
    ],
  },
  {
    id: 'GP5-4',
    title: 'Governance boundary orients zones',
    summary:
      'Governance boundary table + BOUNDARY_RULES remain above zones; each rule answerLayer maps to a visible zone.',
    verifySteps: [
      'Governance boundary section lists Constitution / Spine / Projection layers.',
      'BOUNDARY_RULES answers (e.g. historical sign-off → Spine) match zone names on page.',
    ],
  },
  {
    id: 'GP5-5',
    title: 'Copy Prompt pack order Constitution → Spine → Projection',
    summary:
      'buildBlueprintLlmPack emits sections in governance layer order; Projection MCP inventory follows Spine live state.',
    verifySteps: [
      'Copy Prompt for LLM on Blueprint — sections appear: Constitution, then Spine (if context loaded), then Projection.',
      'Spine pack includes milestone SIGNED semantics note.',
    ],
  },
]

export interface GovernancePhase5ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase5SignoffState {
  version: string
  items: Record<string, GovernancePhase5ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase5_signoff'

function emptyItemState(): GovernancePhase5ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase5SignoffState(): GovernancePhase5SignoffState {
  const items: Record<string, GovernancePhase5ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE5_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE5_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase5SignoffState(): GovernancePhase5SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase5SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase5SignoffState
    if (parsed.version !== GOVERNANCE_PHASE5_VERSION) {
      return defaultPhase5SignoffState()
    }
    const merged = defaultPhase5SignoffState()
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
    return defaultPhase5SignoffState()
  }
}

export function saveGovernancePhase5SignoffState(state: GovernancePhase5SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase5ItemsVerified(state: GovernancePhase5SignoffState): boolean {
  return GOVERNANCE_PHASE5_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase5VerificationCount(state: GovernancePhase5SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE5_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE5_DELIVERY_ITEMS.length }
}

export function isGovernancePhase5SignedOff(): boolean {
  return loadGovernancePhase5SignoffState().signedOffAt != null
}
