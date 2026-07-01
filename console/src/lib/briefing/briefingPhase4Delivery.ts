/** Phase 4 — Program complete: polish + roadmap closure after Phases 1–3. */

import { loadPhase1SignoffState } from '@/lib/briefing/briefingPhase1Delivery'
import { loadPhase2SignoffState } from '@/lib/briefing/briefingPhase2Delivery'
import { loadPhase3SignoffState } from '@/lib/briefing/briefingPhase3Delivery'

export const BRIEFING_PHASE4_VERSION = '2026-07-01'

export interface BriefingPhase4DeliveryItem {
  id: 'R1' | 'R2' | 'R3' | 'R4'
  title: string
  summary: string
  verifySteps: string[]
}

export const BRIEFING_PHASE4_DELIVERY_ITEMS: BriefingPhase4DeliveryItem[] = [
  {
    id: 'R1',
    title: 'Session results in Briefing',
    summary:
      'Closed Agent Desk / briefing sessions appear in Briefing UI (GET /briefing/session-results) — visible S9 read path.',
    verifySteps: [
      'Expand “Session results” — table loads from platform-api (or empty hint on first use).',
      'After Close session on Agent Desk, refresh — new row shows outcome, summary, track/lane.',
      'Row links context to audit briefing.session.close entry.',
    ],
  },
  {
    id: 'R2',
    title: 'IDE-primary delivery channels',
    summary:
      'Copy session pack is the recommended path; Agent Desk prefill is optional and gated for feature/frontend intents.',
    verifySteps: [
      'Generate pack — “Cursor IDE · recommended” column with primary Copy button on the left.',
      'Switch intent to Feature or FE — “Prefill Agent Desk” is disabled with IDE-first tooltip.',
      'Ops/debug/cluster intent — optional Prefill remains available as ghost button.',
    ],
  },
  {
    id: 'R3',
    title: 'Generate flow feedback',
    summary:
      'Generate button sits below reconcile gate; pack ready indicator + auto-scroll to delivery preview.',
    verifySteps: [
      'Click Generate — green “Pack ready” hint appears beside the button.',
      'Page scrolls to delivery channels + pack preview without hunting below the fold.',
      'Button label becomes “Regenerate session briefing” while pack is visible.',
    ],
  },
  {
    id: 'R4',
    title: 'Program roadmap sign-off',
    summary:
      'Owner accepts the three-phase Agent Briefing roadmap (S1–S11 + A1) as delivered — Briefing as JIT projection.',
    verifySteps: [
      'Phase 1–3 sign-off panels show SIGNED (or verify remaining items there first).',
      'Mark R1–R3 verified on this panel.',
      'Admin signs Phase 4 — records program closure in local storage.',
    ],
  },
]

export interface BriefingPhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface BriefingPhase4SignoffState {
  version: string
  items: Record<string, BriefingPhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_briefing_phase4_signoff'

function emptyItemState(): BriefingPhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase4SignoffState(): BriefingPhase4SignoffState {
  const items: Record<string, BriefingPhase4ItemVerification> = {}
  for (const item of BRIEFING_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: BRIEFING_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase4SignoffState(): BriefingPhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase4SignoffState()
    const parsed = JSON.parse(raw) as BriefingPhase4SignoffState
    if (parsed.version !== BRIEFING_PHASE4_VERSION) {
      return defaultPhase4SignoffState()
    }
    const merged = defaultPhase4SignoffState()
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
    return defaultPhase4SignoffState()
  }
}

export function savePhase4SignoffState(state: BriefingPhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase4ItemsVerified(state: BriefingPhase4SignoffState): boolean {
  return BRIEFING_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase4VerificationCount(state: BriefingPhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = BRIEFING_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: BRIEFING_PHASE4_DELIVERY_ITEMS.length }
}

export function priorPhasesSignedOff(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  if (loadPhase1SignoffState().signedOffAt == null) missing.push('Phase 1')
  if (loadPhase2SignoffState().signedOffAt == null) missing.push('Phase 2')
  if (loadPhase3SignoffState().signedOffAt == null) missing.push('Phase 3')
  return { ok: missing.length === 0, missing }
}
