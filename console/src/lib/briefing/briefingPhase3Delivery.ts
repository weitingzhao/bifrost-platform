/** Phase 3 delivery checklist — Owner verifies then signs off in Briefing UI. */

export const BRIEFING_PHASE3_VERSION = '2026-07-01'

export interface BriefingPhase3DeliveryItem {
  id: 'S9' | 'S10' | 'S11' | 'A1'
  title: string
  summary: string
  verifySteps: string[]
}

export const BRIEFING_PHASE3_DELIVERY_ITEMS: BriefingPhase3DeliveryItem[] = [
  {
    id: 'S9',
    title: 'Session close write-back',
    summary:
      'Agent Desk records session outcome to audit (briefing.session.close) when Owner closes a terminal job.',
    verifySteps: [
      'Send a session pack from Briefing → complete or fail a short Agent Desk task.',
      'Click “Close session” on the remediation panel — confirm outcome + summary.',
      'Audit page shows briefing.session.close; GET /briefing/session-results lists the record.',
    ],
  },
  {
    id: 'S10',
    title: 'MCP get_session_briefing',
    summary:
      'MCP tool + GET /api/v1/briefing/session-pack return compact briefing for Agent self-service.',
    verifySteps: [
      'Architecture → MCP Contract — get_session_briefing shows Implemented.',
      'curl GET /api/v1/briefing/session-pack?track=build&lane=console-api&pack=compact returns pack JSON.',
      'MCP server tool get_session_briefing returns same pack (with operator/viewer token).',
    ],
  },
  {
    id: 'S11',
    title: 'Nav-derived UI progress',
    summary:
      'Console UI progress table is generated from sidebar nav registry + uiProgressOverrides (new tabs auto-appear).',
    verifySteps: [
      'Expand “Console UI progress” — row count matches nav tabs (+ API extras).',
      'Each sidebar tab (e.g. Agent Desk, Control Room) has a corresponding row.',
      'Override notes/status still apply (e.g. Briefing = done).',
    ],
  },
  {
    id: 'A1',
    title: 'Cursor automation handoff',
    summary:
      'Copy automation JSON from Briefing — prefill payload for Cursor Automation / zero-click workflow.',
    verifySteps: [
      'Generate session pack → click “Copy automation handoff”.',
      'JSON includes version briefing-automation-v1, meta track/lane/intent, and cursor_automation.prefill.',
      'Paste prefill into a new Cursor chat — Agent receives full pack without manual copy from preview.',
    ],
  },
]

export interface BriefingPhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface BriefingPhase3SignoffState {
  version: string
  items: Record<string, BriefingPhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_briefing_phase3_signoff'

function emptyItemState(): BriefingPhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase3SignoffState(): BriefingPhase3SignoffState {
  const items: Record<string, BriefingPhase3ItemVerification> = {}
  for (const item of BRIEFING_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: BRIEFING_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase3SignoffState(): BriefingPhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase3SignoffState()
    const parsed = JSON.parse(raw) as BriefingPhase3SignoffState
    if (parsed.version !== BRIEFING_PHASE3_VERSION) {
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

export function savePhase3SignoffState(state: BriefingPhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase3ItemsVerified(state: BriefingPhase3SignoffState): boolean {
  return BRIEFING_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase3VerificationCount(state: BriefingPhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = BRIEFING_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: BRIEFING_PHASE3_DELIVERY_ITEMS.length }
}
