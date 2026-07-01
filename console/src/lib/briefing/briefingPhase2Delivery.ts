/** Phase 2 delivery checklist — Owner verifies then signs off in Briefing UI. */

export const BRIEFING_PHASE2_VERSION = '2026-07-01'

export interface BriefingPhase2DeliveryItem {
  id: 'S2' | 'S6' | 'S7' | 'S8'
  title: string
  summary: string
  verifySteps: string[]
}

export const BRIEFING_PHASE2_DELIVERY_ITEMS: BriefingPhase2DeliveryItem[] = [
  {
    id: 'S2',
    title: 'Send session pack to Agent Desk',
    summary:
      'One-click prefill of the generated session briefing into Agent Desk — same ergonomics as drift fix.',
    verifySteps: [
      'Generate a session briefing pack (compact or full).',
      'Click “Send to Agent Desk” — Agent Desk opens with the full pack in the composer.',
      'Confirm you can Send without manual copy-paste.',
    ],
  },
  {
    id: 'S6',
    title: 'Server-side session snapshot',
    summary:
      'Copy/generate saves baseline to platform-api (POST /session-snapshots); Session delta reads GET /latest.',
    verifySteps: [
      'Copy session pack or generate — network tab shows POST /api/v1/session-snapshots (operator token).',
      'Refresh Briefing — Session delta panel loads baseline from server (not only localStorage).',
      'Clear browser localStorage for bifrost_session_snapshot — delta still works if server snapshot exists.',
    ],
  },
  {
    id: 'S7',
    title: 'Automation panels collapsed by default',
    summary:
      'Sync loop, nightly report, session delta, and UI progress fold away so active lane work is above the fold.',
    verifySteps: [
      'Open Briefing — automation sections (sync loop, nightly, session delta, UI progress) are collapsed.',
      'Expand each section — content unchanged, only default visibility improved.',
      'Track/lane queue and session pack generator remain visible without scrolling.',
    ],
  },
  {
    id: 'S8',
    title: 'Per-lane reconcile scope',
    summary:
      'Migrate stream blockers apply only to the selected migrate lane; other lanes downgrade to warnings.',
    verifySteps: [
      'Select a Build lane — migrate structural drift (if any) shows as warning, pack not blocked.',
      'Select trade-k8s-native — trade stream queue/spine mismatches remain blockers.',
      'Select data-layer-k3s while trade stream has drift — trade-only blockers become warnings.',
    ],
  },
]

export interface BriefingPhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface BriefingPhase2SignoffState {
  version: string
  items: Record<string, BriefingPhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_briefing_phase2_signoff'

function emptyItemState(): BriefingPhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase2SignoffState(): BriefingPhase2SignoffState {
  const items: Record<string, BriefingPhase2ItemVerification> = {}
  for (const item of BRIEFING_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: BRIEFING_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase2SignoffState(): BriefingPhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase2SignoffState()
    const parsed = JSON.parse(raw) as BriefingPhase2SignoffState
    if (parsed.version !== BRIEFING_PHASE2_VERSION) {
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

export function savePhase2SignoffState(state: BriefingPhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase2ItemsVerified(state: BriefingPhase2SignoffState): boolean {
  return BRIEFING_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase2VerificationCount(state: BriefingPhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = BRIEFING_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: BRIEFING_PHASE2_DELIVERY_ITEMS.length }
}
