/** Phase 1 delivery checklist — Owner verifies then signs off in Briefing UI. */

export const BRIEFING_PHASE1_VERSION = '2026-07-01'

export interface BriefingPhase1DeliveryItem {
  id: 'S1' | 'S3' | 'S4' | 'S5'
  title: string
  summary: string
  verifySteps: string[]
}

export const BRIEFING_PHASE1_DELIVERY_ITEMS: BriefingPhase1DeliveryItem[] = [
  {
    id: 'S1',
    title: 'Done items collapsed in lane queue',
    summary: 'Completed queue items fold into a collapsible group so active work stays visible.',
    verifySteps: [
      'Open a migrate lane with signed waves (e.g. trade-k8s-native).',
      'Confirm completed items are hidden under a “N completed” collapsible row (default collapsed).',
      'Expand the group — done/closed items appear with muted styling.',
    ],
  },
  {
    id: 'S3',
    title: 'URL state persistence + deep link',
    summary: 'Track, lane, intent, and pack size sync to ?track=&lane=&intent=&pack= in the URL.',
    verifySteps: [
      'Change track/lane/intent/pack — URL query updates without losing #briefing hash.',
      'Refresh the page — selections restore from the URL.',
      'From Control Room, click a work track card — Briefing opens with ?track= preset.',
    ],
  },
  {
    id: 'S4',
    title: 'Compact / Full session pack',
    summary: 'Default compact pack omits UI progress table, vision appendix, and full track rollup.',
    verifySteps: [
      'Pack size toggle shows Compact (default) and Full.',
      'Generate compact pack — char count is materially smaller than Full.',
      'Compact pack still includes lane queue (active only), read-first, and live snapshot.',
    ],
  },
  {
    id: 'S5',
    title: 'Work intent override',
    summary: 'Lane sets the default intent; Owner can override via SegmentControl before generating.',
    verifySteps: [
      'Select a lane — default intent matches lane mapping.',
      'Override intent (e.g. Debug on a Feature lane) — pack header shows overridden intent.',
      'URL ?intent= updates when override differs from lane default.',
    ],
  },
]

export interface BriefingPhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface BriefingPhase1SignoffState {
  version: string
  items: Record<string, BriefingPhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_briefing_phase1_signoff'

function emptyItemState(): BriefingPhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase1SignoffState(): BriefingPhase1SignoffState {
  const items: Record<string, BriefingPhase1ItemVerification> = {}
  for (const item of BRIEFING_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: BRIEFING_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase1SignoffState(): BriefingPhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase1SignoffState()
    const parsed = JSON.parse(raw) as BriefingPhase1SignoffState
    if (parsed.version !== BRIEFING_PHASE1_VERSION) {
      return defaultPhase1SignoffState()
    }
    const merged = defaultPhase1SignoffState()
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
    return defaultPhase1SignoffState()
  }
}

export function savePhase1SignoffState(state: BriefingPhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase1ItemsVerified(state: BriefingPhase1SignoffState): boolean {
  return BRIEFING_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase1VerificationCount(state: BriefingPhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = BRIEFING_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: BRIEFING_PHASE1_DELIVERY_ITEMS.length }
}
