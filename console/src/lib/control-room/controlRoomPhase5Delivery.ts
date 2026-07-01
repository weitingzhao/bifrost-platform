/** Control Room Phase 5 delivery checklist — Promote / cutover (launch window). */

import { notifyControlRoomSignoffChanged } from './controlRoomSignoffEvents'

export const CONTROL_ROOM_PHASE5_VERSION = '2026-07-01'

export interface ControlRoomPhase5DeliveryItem {
  id: 'P5-1' | 'P5-2' | 'P5-3' | 'P5-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE5_DELIVERY_ITEMS: ControlRoomPhase5DeliveryItem[] = [
  {
    id: 'P5-1',
    title: 'Promote / cutover strip in diagnosis zone',
    summary:
      'Launch-window strip at the bottom of diagnosis shows STG release + Prod cutover tracks with the same lamps as Promote page.',
    verifySteps: [
      'Open Control Room — “Promote / cutover” section appears after Mission timeline.',
      'Prod cutover row shows Promote blocked or Promote ready (narrative) matching Operate → Promote.',
      'When stg smoke is loaded, STG release track appears beside Prod cutover.',
    ],
  },
  {
    id: 'P5-2',
    title: 'Spine focus and milestone blocker alignment',
    summary:
      'When 2c-b-prod-cutover is BLOCKED_ON, spine focus blocker and milestone blocker are shown together with alignment note.',
    verifySteps: [
      'With coupling blocked, strip shows Spine focus headline and blocker line.',
      'Milestone blocker matches Promote page cutover.blocker when BLOCKED_ON.',
      'Alignment note explains match or mismatch between focus and milestone.',
    ],
  },
  {
    id: 'P5-3',
    title: 'Go to Promote with preflight pack',
    summary:
      'One-click Go to Promote stashes Tier A/B preflight pack; Promote page shows preflight banner; Copy preflight works in Control Room.',
    verifySteps: [
      'Click “Go to Promote (preflight)” — navigates to Promote tab.',
      'Promote page shows preflight banner from Control Room (dismiss or copy again).',
      'Click “Copy preflight” in Control Room — clipboard contains Flywheel A/B checklist + promote status.',
    ],
  },
  {
    id: 'P5-4',
    title: 'Coupling gate parity with Promote page',
    summary:
      'Primary blocked reason and full reasons list match evaluatePromoteStatus on both Control Room and Promote.',
    verifySteps: [
      'With prod matrix failing, both pages show “Prod matrix has failing targets” as primary or listed reason.',
      'Payload coupling hint still present; strip adds diagnosis-edge launch window (not duplicate-only in Program context).',
      'When gate opens, both show narrative ready / coupling gate open.',
    ],
  },
]

export interface ControlRoomPhase5ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase5SignoffState {
  version: string
  items: Record<string, ControlRoomPhase5ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase5_signoff'

function emptyItemState(): ControlRoomPhase5ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase5SignoffState(): ControlRoomPhase5SignoffState {
  const items: Record<string, ControlRoomPhase5ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE5_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE5_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase5SignoffState(): ControlRoomPhase5SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase5SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase5SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE5_VERSION) {
      return defaultPhase5SignoffState()
    }
    const merged = defaultPhase5SignoffState()
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
    return defaultPhase5SignoffState()
  }
}

export function savePhase5SignoffState(state: ControlRoomPhase5SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyControlRoomSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allPhase5ItemsVerified(state: ControlRoomPhase5SignoffState): boolean {
  return CONTROL_ROOM_PHASE5_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase5VerificationCount(state: ControlRoomPhase5SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE5_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE5_DELIVERY_ITEMS.length }
}

export function isControlRoomProgramComplete(): boolean {
  return loadPhase5SignoffState().signedOffAt != null
}
