/** Control Room Phase 4 delivery checklist — Mission timeline (accountability & memory). */

export const CONTROL_ROOM_PHASE4_VERSION = '2026-07-01'

export interface ControlRoomPhase4DeliveryItem {
  id: 'P4-1' | 'P4-2' | 'P4-3' | 'P4-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE4_DELIVERY_ITEMS: ControlRoomPhase4DeliveryItem[] = [
  {
    id: 'P4-1',
    title: 'Mission timeline strip in diagnosis zone',
    summary:
      'Collapsible timeline under Command intent shows probe → Agent → approval → verify narrative for the last 24h.',
    verifySteps: [
      'Open Control Room — expand “Mission timeline” under Command intent.',
      'Events list includes Agent started / approval / finished rows when jobs ran recently.',
      'Current probe degradation appears as “Mission probes degraded” when matrix is not green.',
    ],
  },
  {
    id: 'P4-2',
    title: 'Audit deep-link from timeline events',
    summary: 'Actuation rows expose Open Audit; remediation rows link job target to audit when recorded.',
    verifySteps: [
      'Click “Open Audit” in the timeline header — Audit tab opens.',
      'On an audit actuation row, click “Audit” — same Audit tab.',
      'Remediation.start / done / approval entries appear when platform-api recorded them.',
    ],
  },
  {
    id: 'P4-3',
    title: 'Nightly / drift summary line',
    summary: 'Engineer nightly Layer 1–3 conclusion rendered as a one-line summary above the event list.',
    verifySteps: [
      'When nightly report is available, a drift summary line shows L1–L3 pass/fail and timestamp.',
      'When unavailable, hint explains awaiting Mac Mini nightly run.',
      'Nightly drift event appears in the timeline when report generated_at is present.',
    ],
  },
  {
    id: 'P4-4',
    title: 'Full Agent task trajectory replay',
    summary:
      'Multi-step jobs (e.g. release-agent-task) show a Replay trajectory chip that opens Agent Desk on the job.',
    verifySteps: [
      'After a release-scoped Agent run, timeline shows Trajectories with scope label and status.',
      'Click “Replay trajectory” — Agent Desk opens focused on that job id.',
      'Individual event rows also offer “Agent Desk” for the linked job.',
    ],
  },
]

export interface ControlRoomPhase4ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase4SignoffState {
  version: string
  items: Record<string, ControlRoomPhase4ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase4_signoff'

function emptyItemState(): ControlRoomPhase4ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase4SignoffState(): ControlRoomPhase4SignoffState {
  const items: Record<string, ControlRoomPhase4ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE4_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE4_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase4SignoffState(): ControlRoomPhase4SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase4SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase4SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE4_VERSION) {
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

export function savePhase4SignoffState(state: ControlRoomPhase4SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase4ItemsVerified(state: ControlRoomPhase4SignoffState): boolean {
  return CONTROL_ROOM_PHASE4_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase4VerificationCount(state: ControlRoomPhase4SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE4_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE4_DELIVERY_ITEMS.length }
}
