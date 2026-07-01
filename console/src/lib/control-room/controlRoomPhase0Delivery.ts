/** Control Room Phase 0 delivery checklist — Owner verifies then signs off in UI. */

export const CONTROL_ROOM_PHASE0_VERSION = '2026-07-01'

export interface ControlRoomPhase0DeliveryItem {
  id: 'P0-1' | 'P0-2' | 'P0-3'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE0_DELIVERY_ITEMS: ControlRoomPhase0DeliveryItem[] = [
  {
    id: 'P0-1',
    title: 'Diagnosis zone — mission board, payload, active Agent jobs',
    summary:
      'Upper Control Room shows mission status, Trade payload matrix, and live Agent job strip — no governance clutter above the fold.',
    verifySteps: [
      'Open Control Room — mission board and Payload table appear first.',
      'Active Agent jobs strip sits directly under the payload table (empty state or running/awaiting chips).',
      'Rocket subsystem cards are not duplicated in the diagnosis zone (they live under Program context).',
    ],
  },
  {
    id: 'P0-2',
    title: 'Program context — collapsed governance',
    summary:
      'Work tracks, dual flywheel, pipeline, and Agent focus dock fold into a single “Program context” section (default collapsed after sign-off).',
    verifySteps: [
      'Expand “Program context” — Work tracks, Dual flywheel intro/panel, Pipeline, and Agent focus dock are inside.',
      'Collapse the section — diagnosis zone stays compact.',
      'Work track cards still deep-link to Briefing with ?track= preset.',
    ],
  },
  {
    id: 'P0-3',
    title: 'FocusStrip dedup — env + mission one-liner',
    summary:
      'Global strip no longer repeats Rocket four-module pills; Trade env dots + mission status + Fix + Control Room entry only.',
    verifySteps: [
      'On any page with FocusStrip — row shows Trade dev/prod, mission status, Fix (when degraded), Control Room.',
      'Rocket Infra/Release/Control/Agent pills are absent from the strip row.',
      'Expand strip chevron — payload detail only; full rocket telemetry is in Control Room.',
    ],
  },
]

export interface ControlRoomPhase0ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase0SignoffState {
  version: string
  items: Record<string, ControlRoomPhase0ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase0_signoff'

function emptyItemState(): ControlRoomPhase0ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase0SignoffState(): ControlRoomPhase0SignoffState {
  const items: Record<string, ControlRoomPhase0ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE0_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE0_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase0SignoffState(): ControlRoomPhase0SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase0SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase0SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE0_VERSION) {
      return defaultPhase0SignoffState()
    }
    const merged = defaultPhase0SignoffState()
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
    return defaultPhase0SignoffState()
  }
}

export function savePhase0SignoffState(state: ControlRoomPhase0SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase0ItemsVerified(state: ControlRoomPhase0SignoffState): boolean {
  return CONTROL_ROOM_PHASE0_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase0VerificationCount(state: ControlRoomPhase0SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE0_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE0_DELIVERY_ITEMS.length }
}
