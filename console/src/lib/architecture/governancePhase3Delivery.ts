/** Governance boundary program — Phase 3 Spine semantics delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const GOVERNANCE_PHASE3_VERSION = '2026-07-01'

export interface GovernancePhase3DeliveryItem {
  id: 'GP3-1' | 'GP3-2' | 'GP3-3' | 'GP3-4' | 'GP3-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE3_DELIVERY_ITEMS: GovernancePhase3DeliveryItem[] = [
  {
    id: 'GP3-1',
    title: 'ops-context.yaml milestone status semantics',
    summary:
      'Spine file header documents SIGNED / CLOSED / IN_PROGRESS meanings and SIGNED ≠ live gate ready.',
    verifySteps: [
      'config/ops-context.yaml top comments include Milestone status semantics block.',
      'Comment states SIGNED ≠ currently ready — live readiness from Projection.',
    ],
  },
  {
    id: 'GP3-2',
    title: 'Program Milestones historical labels',
    summary:
      'Architecture → Milestones table shows SIGNED (historically) / CLOSED (archived) — not raw gate verdict.',
    verifySteps: [
      'Architecture → Milestones — Status column uses historical qualifiers for SIGNED/CLOSED.',
      'Section footnote explains live gate readiness lives on Promote (Projection).',
    ],
  },
  {
    id: 'GP3-3',
    title: 'Promote dual labels when SIGNED but gate pending',
    summary:
      'Operate → Promote shows spine + gate chips for 2c-b-prod-cutover when historically SIGNED but promote not ready.',
    verifySteps: [
      'Promote page — Prod cutover spine semantics panel visible when cutover is SIGNED.',
      'Dual chips: SIGNED (historically) + gate: pending (current prod matrix / gate state).',
      'No contradiction — milestone signed does not imply Promote ready.',
    ],
  },
  {
    id: 'GP3-4',
    title: 'Control Room Promote strip aligned semantics',
    summary:
      'Control Room Promote / cutover strip notes SIGNED (historically) vs live gate pending when applicable.',
    verifySteps: [
      'Control Room — Promote / cutover strip shows dual tags when cutover milestone is SIGNED.',
      'Alignment note mentions historical sign-off vs Projection gate.',
    ],
  },
  {
    id: 'GP3-5',
    title: 'Blueprint LLM pack spine semantics note',
    summary:
      'Copy Prompt for LLM Spine section includes SIGNED ≠ live gate readiness disclaimer.',
    verifySteps: [
      'Architecture → Blueprint → Copy Prompt for LLM — Spine section contains semantics note.',
      'Blueprint Spine snapshot section repeats the same disclaimer when context is loaded.',
    ],
  },
]

export interface GovernancePhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase3SignoffState {
  version: string
  items: Record<string, GovernancePhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase3_signoff'

function emptyItemState(): GovernancePhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase3SignoffState(): GovernancePhase3SignoffState {
  const items: Record<string, GovernancePhase3ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase3SignoffState(): GovernancePhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase3SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase3SignoffState
    if (parsed.version !== GOVERNANCE_PHASE3_VERSION) {
      return defaultPhase3SignoffState()
    }
    const merged = defaultPhase3SignoffState()
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
    return defaultPhase3SignoffState()
  }
}

export function saveGovernancePhase3SignoffState(state: GovernancePhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase3ItemsVerified(state: GovernancePhase3SignoffState): boolean {
  return GOVERNANCE_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase3VerificationCount(state: GovernancePhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE3_DELIVERY_ITEMS.length }
}

export function isGovernancePhase3SignedOff(): boolean {
  return loadGovernancePhase3SignoffState().signedOffAt != null
}
