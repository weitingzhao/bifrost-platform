/** Governance boundary program — Phase 2 Projection placement delivery checklist. */

import { notifyGovernanceSignoffChanged } from './governanceSignoffEvents'

export const GOVERNANCE_PHASE2_VERSION = '2026-07-01'

export interface GovernancePhase2DeliveryItem {
  id: 'GP2-1' | 'GP2-2' | 'GP2-3' | 'GP2-4' | 'GP2-5'
  title: string
  summary: string
  verifySteps: string[]
}

export const GOVERNANCE_PHASE2_DELIVERY_ITEMS: GovernancePhase2DeliveryItem[] = [
  {
    id: 'GP2-1',
    title: 'Delivery prod-cutover status from spine',
    summary:
      'STG release workflow prod-cutover phase status is derived from spine milestone 2c-b-prod-cutover — not hardcoded active.',
    verifySteps: [
      'Operate → Delivery → Blueprint tab → STG release workflow phases.',
      'Prod cutover row shows status done when spine milestone 2c-b-prod-cutover is SIGNED.',
      'Spine chip on row shows milestone id + live status (e.g. 2c-b-prod-cutover · SIGNED).',
    ],
  },
  {
    id: 'GP2-2',
    title: 'Delivery catalog summaries without stale progress text',
    summary:
      'deliveryMainlineCatalog phase summaries describe scope only — no IN_PROGRESS / SIGNED dates embedded in catalog text.',
    verifySteps: [
      'Prod cutover summary does not contain “IN_PROGRESS” or hardcoded sign-off dates.',
      'Copy for LLM / delivery pack uses resolveStgReleasePhases(context) for [status] tags.',
    ],
  },
  {
    id: 'GP2-3',
    title: 'Environments PLATFORM scope notes purified',
    summary: 'SCOPE PLATFORM row describes role only — no Phase 0 / L0 read-only implementation stage text.',
    verifySteps: [
      'Architecture → Environments → Scope table — PLATFORM notes mention control plane role only.',
      'No “Phase 0 L0 read-only” or “Future: agent/, mcp/” in PLATFORM notes.',
    ],
  },
  {
    id: 'GP2-4',
    title: 'Environments platform phases use sequence',
    summary:
      'PLATFORM_PHASES table uses Sequence (First / Second / Third) — no calendar time boxes like “now ~3mo”.',
    verifySteps: [
      'Environments page Platform phases section column is Sequence.',
      'Rows show First / Second / Third without month ranges.',
    ],
  },
  {
    id: 'GP2-5',
    title: 'Projection source label on Delivery workflow',
    summary:
      'Delivery Blueprint tab labels phase status as Projection (spine) with link hint to Milestones.',
    verifySteps: [
      'STG release workflow section notes “Phase status from spine (Projection)”.',
      'Each phase row shows spine milestone chip when context is loaded.',
    ],
  },
]

export interface GovernancePhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface GovernancePhase2SignoffState {
  version: string
  items: Record<string, GovernancePhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_governance_phase2_signoff'

function emptyItemState(): GovernancePhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase2SignoffState(): GovernancePhase2SignoffState {
  const items: Record<string, GovernancePhase2ItemVerification> = {}
  for (const item of GOVERNANCE_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: GOVERNANCE_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadGovernancePhase2SignoffState(): GovernancePhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase2SignoffState()
    const parsed = JSON.parse(raw) as GovernancePhase2SignoffState
    if (parsed.version !== GOVERNANCE_PHASE2_VERSION) {
      return defaultPhase2SignoffState()
    }
    const merged = defaultPhase2SignoffState()
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
    return defaultPhase2SignoffState()
  }
}

export function saveGovernancePhase2SignoffState(state: GovernancePhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyGovernanceSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allGovernancePhase2ItemsVerified(state: GovernancePhase2SignoffState): boolean {
  return GOVERNANCE_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function governancePhase2VerificationCount(state: GovernancePhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = GOVERNANCE_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: GOVERNANCE_PHASE2_DELIVERY_ITEMS.length }
}

export function isGovernancePhase2SignedOff(): boolean {
  return loadGovernancePhase2SignoffState().signedOffAt != null
}
