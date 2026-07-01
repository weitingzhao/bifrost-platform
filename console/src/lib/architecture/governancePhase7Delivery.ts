/** Governance boundary program — Phase 7 Program closure & doctrine sync delivery checklist. */

import {
  GOVERNANCE_PROGRAM_PHASES,
  isGovernancePhaseSignedOff,
} from './governanceProgramStatus'
import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const GOVERNANCE_PHASE7_VERSION = '2026-07-01'

export interface GovernancePhase7DeliveryItem {
  id: 'GP7-1' | 'GP7-2' | 'GP7-3' | 'GP7-4' | 'GP7-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE7_DELIVERY_ITEMS: GovernancePhase7DeliveryItem[] = [
  {
    id: 'GP7-1',
    title: 'Governance program status strip',
    summary:
      'Blueprint shows P1–P6 sign-off progress strip — all six phases marked ✓ when Owner has signed each phase panel.',
    verifySteps: [
      'Architecture → Blueprint — “Governance program” strip shows P1–P6 tags.',
      'Each tag shows ✓ when that phase is signed off in its home panel.',
      'When all six signed, strip reads “All phases signed — ready for Phase 7 program closure”.',
    ],
  },
  {
    id: 'GP7-2',
    title: 'Reconcile doctrine synced post-P6',
    summary:
      'Briefing Reconciliation gate-catalog-spine-parity documents Constitution check (no progress prose on spine-bound rows).',
    verifySteps: [
      'Agent → Briefing Reconciliation → Reconcile gate table — gate-catalog-spine-parity condition mentions Constitution / spine-bound rows.',
      'Catalog ↔ spine drift panel shows SYNCED (not legacy IN_PROGRESS vs SIGNED sample).',
    ],
  },
  {
    id: 'GP7-3',
    title: 'Anti-pattern catalog updated',
    summary:
      'deployMainline hardcoded progress anti-pattern marked resolved — fix points to Governance Phase 6 Projection pattern.',
    verifySteps: [
      'Briefing Reconciliation → Anti-patterns — deployMainline entry status shows RESOLVED (Governance P6).',
      'Fix text references spineMilestoneId + resolveMainlinePhases(context).',
    ],
  },
  {
    id: 'GP7-4',
    title: 'Six phase panels reachable',
    summary:
      'Owner can navigate to every phase sign-off panel: P1/P3/P5 Blueprint, P2 Delivery, P4/P6 Briefing Reconciliation.',
    verifySteps: [
      'Blueprint — Phase 1, 3, 5 panels visible (historical SIGNED ok).',
      'Operate → Delivery — Phase 2 panel visible.',
      'Agent → Briefing Reconciliation — Phase 4 and Phase 6 panels visible.',
    ],
  },
  {
    id: 'GP7-5',
    title: 'Governance program closure sign-off',
    summary:
      'Owner accepts Constitution / Spine / Projection boundary program (P1–P6) as delivered — maintenance mode for governance layer.',
    verifySteps: [
      'All P1–P6 phase panels show SIGNED before enabling Phase 7 sign-off.',
      'Sign off Phase 7 — panel shows GOVERNANCE PROGRAM COMPLETE.',
      'Future catalog drift remediations are event-driven, not part of this program.',
    ],
  },
]

export interface GovernancePhase7ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase7SignoffState {
  version: string
  items: Record<string, GovernancePhase7ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase7_signoff'

function emptyItemState(): GovernancePhase7ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase7SignoffState(): GovernancePhase7SignoffState {
  const items: Record<string, GovernancePhase7ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE7_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE7_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase7SignoffState(): GovernancePhase7SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase7SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase7SignoffState
    if (parsed.version !== GOVERNANCE_PHASE7_VERSION) {
      return defaultPhase7SignoffState()
    }
    const merged = defaultPhase7SignoffState()
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
    return defaultPhase7SignoffState()
  }
}

export function saveGovernancePhase7SignoffState(state: GovernancePhase7SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase7ItemsVerified(state: GovernancePhase7SignoffState): boolean {
  return GOVERNANCE_PHASE7_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase7VerificationCount(state: GovernancePhase7SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE7_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE7_DELIVERY_ITEMS.length }
}

export function isGovernancePhase7SignedOff(): boolean {
  return loadGovernancePhase7SignoffState().signedOffAt != null
}

export function priorGovernancePhasesSignedOff(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  for (const p of GOVERNANCE_PROGRAM_PHASES) {
    if (!isGovernancePhaseSignedOff(p.id)) {
      missing.push(`${p.id} ${p.shortLabel}`)
    }
  }
  return { ok: missing.length === 0, missing }
}
