/** Control Room Phase 2 delivery checklist — Payload depth (Trade readiness + coupling). */

import { notifyControlRoomSignoffChanged } from './controlRoomSignoffEvents'

export const CONTROL_ROOM_PHASE2_VERSION = '2026-07-01'

export interface ControlRoomPhase2DeliveryItem {
  id: 'P2-1' | 'P2-2' | 'P2-3' | 'P2-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE2_DELIVERY_ITEMS: ControlRoomPhase2DeliveryItem[] = [
  {
    id: 'P2-1',
    title: 'Trade readiness rows — Daemon / Celery / IB / datastore / UI',
    summary:
      'Payload diagnosis shows component-level readiness mapped from matrix probes (monitor, ops, policy-blocked IB, PG/Redis, SPA).',
    verifySteps: [
      'Open Control Room — under Payload, expand Trade readiness table.',
      'Rows: Daemon (api-monitor), Celery/Ops (api-ops), IB edge (L0 blocked), PG/Redis, Trade UI.',
      'Status lamps reflect current matrix reachability (not aggregate env only).',
    ],
  },
  {
    id: 'P2-2',
    title: 'Dev vs prod comparison',
    summary:
      'Each readiness row shows dev and prod columns side-by-side; diverging rows are highlighted with a delta badge.',
    verifySteps: [
      'Compare dev and prod columns for each component row.',
      'When dev and prod signals differ, row is highlighted and header shows “N dev/prod delta(s)”.',
      'Datastore row aggregates postgres + redis per environment.',
    ],
  },
  {
    id: 'P2-3',
    title: 'Coupling gate hint in diagnosis zone',
    summary:
      'Promote / release-gate coupling appears under Payload (not only in Program context flywheel).',
    verifySteps: [
      'With prod matrix failing or gate not pass, coupling hint shows “Coupling gate blocked” + primary reason.',
      'Delivery button opens Delivery page; Program button appears when milestone blocked.',
      'When gate is open, hint shows “Coupling gate open”.',
    ],
  },
  {
    id: 'P2-4',
    title: 'Runtime Map drill-down from readiness',
    summary: 'Non-policy rows link to Runtime Map for the failing environment.',
    verifySteps: [
      'On a failing component row, click Map — Runtime Map opens with env preset.',
      'IB edge row shows L0 blocked (no Map link — platform must not probe write path).',
    ],
  },
]

export interface ControlRoomPhase2ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase2SignoffState {
  version: string
  items: Record<string, ControlRoomPhase2ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase2_signoff'

function emptyItemState(): ControlRoomPhase2ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase2SignoffState(): ControlRoomPhase2SignoffState {
  const items: Record<string, ControlRoomPhase2ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE2_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE2_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase2SignoffState(): ControlRoomPhase2SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase2SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase2SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE2_VERSION) {
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

export function savePhase2SignoffState(state: ControlRoomPhase2SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyControlRoomSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allPhase2ItemsVerified(state: ControlRoomPhase2SignoffState): boolean {
  return CONTROL_ROOM_PHASE2_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase2VerificationCount(state: ControlRoomPhase2SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE2_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE2_DELIVERY_ITEMS.length }
}
