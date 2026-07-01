/** Control Room Phase 3 delivery checklist — Command intent (mission-scoped commander actions). */

import { notifyControlRoomSignoffChanged } from './controlRoomSignoffEvents'

export const CONTROL_ROOM_PHASE3_VERSION = '2026-07-01'

export interface ControlRoomPhase3DeliveryItem {
  id: 'P3-1' | 'P3-2' | 'P3-3' | 'P3-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE3_DELIVERY_ITEMS: ControlRoomPhase3DeliveryItem[] = [
  {
    id: 'P3-1',
    title: 'Command intent strip in diagnosis zone',
    summary:
      'Mission-scoped command row lives above the fold (not only in collapsed Program context Agent dock).',
    verifySteps: [
      'Open Control Room — “Command intent” section appears under Agent loop in diagnosis zone.',
      'Shows suggested Agent mode tag (Product / Ops / Promote).',
      'Spine focus headline visible when context is loaded.',
    ],
  },
  {
    id: 'P3-2',
    title: 'Context-aware intent chips',
    summary:
      'Primary chips adapt to mission: payload fail → Verify payload; release fail → Review release + Platform release; promote blocked → Assess promote.',
    verifySteps: [
      'With Payload CRITICAL, “Verify payload” chip is primary.',
      'With Release failed, “Review release” and “Platform release (Agent)” chips appear.',
      'With coupling blocked, “Assess promote” chip prefills Promote pack.',
    ],
  },
  {
    id: 'P3-3',
    title: 'One-click Agent Desk prefill from intent',
    summary:
      'Intent chips open Agent Desk with session pack, dispatch brief, or release prompt — no empty composer.',
    verifySteps: [
      'Click “Agent session pack” — Agent Desk opens with session pack + starter prompt in composer.',
      'Click “Verify payload” (when degraded) — composer includes Control Room dispatch brief sections.',
      'Click “Platform release (Agent)” when Release failed — release scope prompt prefilled.',
    ],
  },
  {
    id: 'P3-4',
    title: 'Copy governance packs from diagnosis zone',
    summary: 'Copy Session / Ops / Promote packs without expanding Program context Agent focus dock.',
    verifySteps: [
      'Click Copy Session — clipboard contains spine + matrix summary (toast “Copied”).',
      'Copy Ops and Copy Promote work when spine context is loaded.',
      'Agent Briefing chip opens Briefing with operate or build track preset.',
    ],
  },
]

export interface ControlRoomPhase3ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase3SignoffState {
  version: string
  items: Record<string, ControlRoomPhase3ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase3_signoff'

function emptyItemState(): ControlRoomPhase3ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase3SignoffState(): ControlRoomPhase3SignoffState {
  const items: Record<string, ControlRoomPhase3ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE3_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE3_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase3SignoffState(): ControlRoomPhase3SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase3SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase3SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE3_VERSION) {
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

export function savePhase3SignoffState(state: ControlRoomPhase3SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyControlRoomSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allPhase3ItemsVerified(state: ControlRoomPhase3SignoffState): boolean {
  return CONTROL_ROOM_PHASE3_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase3VerificationCount(state: ControlRoomPhase3SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE3_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE3_DELIVERY_ITEMS.length }
}
