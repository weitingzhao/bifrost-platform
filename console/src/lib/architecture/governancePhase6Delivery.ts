/** Governance boundary program — Phase 6 Catalog projection cleanup delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const GOVERNANCE_PHASE6_VERSION = '2026-07-01'

export interface GovernancePhase6DeliveryItem {
  id: 'GP6-1' | 'GP6-2' | 'GP6-3' | 'GP6-4' | 'GP6-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE6_DELIVERY_ITEMS: GovernancePhase6DeliveryItem[] = [
  {
    id: 'GP6-1',
    title: 'Deploy Mainline Constitution — spine-bound rows only',
    summary:
      'deployMainlineCatalog seq 4/5/7 use spineMilestoneId only — no embedded IN_PROGRESS / SIGNED progress prose in catalog source.',
    verifySteps: [
      'Open deployMainlineCatalog.ts — seq 4/5/7 rows have spineMilestoneId, no status or historicalNote with live progress.',
      'MAINLINE_PHASE_DEFINITIONS is the authoritative structure; deprecated MAINLINE_PHASES is fallback only.',
    ],
  },
  {
    id: 'GP6-2',
    title: 'Deploy Mainline page live Projection',
    summary:
      'Operate → Deploy Mainline resolves status via resolveMainlinePhases(context) from GET /api/v1/context.',
    verifySteps: [
      'Open Operate → Deploy Mainline — Mainline phases table shows “Projection ← spine” note.',
      'Seq 5 (2c-b-prod-cutover) status tag matches spine (e.g. SIGNED), not hardcoded IN_PROGRESS.',
      'Spine milestone id secondary tag appears on seq 4/5/7 rows when context loads.',
    ],
  },
  {
    id: 'GP6-3',
    title: 'Briefing Reconciliation CATALOG_DRIFT cleared',
    summary:
      'gate-catalog-spine-parity checks Constitution (no progress prose on spine-bound rows) — live panel shows SYNCED.',
    verifySteps: [
      'Open Agent → Briefing Reconciliation — Catalog ↔ spine drift panel shows SYNCED (green).',
      'No CATALOG_DRIFT finding for deployMainline seq 5 vs 2c-b-prod-cutover.',
    ],
  },
  {
    id: 'GP6-4',
    title: 'scan_layer3 Constitution mirror',
    summary:
      'agent/drift/scan_layer3.py mirrors catalogSpineParity constitution check — 0 catalog_spine_parity findings when clean.',
    verifySteps: [
      'Run: python3 agent/drift/scan_layer3.py — no catalog_spine_parity section (or 0 findings).',
      'Offline scan no longer compares deprecated MAINLINE_PHASES status strings to spine.',
    ],
  },
  {
    id: 'GP6-5',
    title: 'Copy Prompt pack uses resolved phases',
    summary:
      'buildDeployMainlineLlmPack(context) emits live spine status for seq 4/5/7 when context is available.',
    verifySteps: [
      'Deploy Mainline → Copy Prompt for LLM — seq 5 line includes spine:2c-b-prod-cutover=SIGNED (or current spine value).',
      'Pack header states Constitution catalog — live milestone progress from spine (Projection).',
    ],
  },
]

export interface GovernancePhase6ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase6SignoffState {
  version: string
  items: Record<string, GovernancePhase6ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase6_signoff'

function emptyItemState(): GovernancePhase6ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase6SignoffState(): GovernancePhase6SignoffState {
  const items: Record<string, GovernancePhase6ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE6_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE6_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase6SignoffState(): GovernancePhase6SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase6SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase6SignoffState
    if (parsed.version !== GOVERNANCE_PHASE6_VERSION) {
      return defaultPhase6SignoffState()
    }
    const merged = defaultPhase6SignoffState()
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
    return defaultPhase6SignoffState()
  }
}

export function saveGovernancePhase6SignoffState(state: GovernancePhase6SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase6ItemsVerified(state: GovernancePhase6SignoffState): boolean {
  return GOVERNANCE_PHASE6_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase6VerificationCount(state: GovernancePhase6SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE6_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE6_DELIVERY_ITEMS.length }
}

export function isGovernancePhase6SignedOff(): boolean {
  return loadGovernancePhase6SignoffState().signedOffAt != null
}
