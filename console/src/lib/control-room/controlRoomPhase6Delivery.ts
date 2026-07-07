/** Control Room Phase 6 delivery checklist — Launch Pad (release + satellite deploy). */

import { notifyControlRoomSignoffChanged } from './controlRoomSignoffEvents'

export const CONTROL_ROOM_PHASE6_VERSION = '2026-07-07'

export interface ControlRoomPhase6DeliveryItem {
  id: 'P6-1' | 'P6-2' | 'P6-3' | 'P6-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE6_DELIVERY_ITEMS: ControlRoomPhase6DeliveryItem[] = [
  {
    id: 'P6-1',
    title: 'Launch Pad cards above diagnosis fold',
    summary:
      'Rocket Launch and Satellite Deploy cards appear directly under the mission header — primary launch surface for Ops Platform and Trade STG.',
    verifySteps: [
      'Open Control Room — two Launch Pad cards (Rocket Launch · Satellite Deploy) sit under MissionControlHeader.',
      'Cards use reach lamps and dense summary lines (no duplicate Rocket subsystem grid above the fold).',
      'Program context section stays collapsed by default; Launch Pad is not buried inside it.',
    ],
  },
  {
    id: 'P6-2',
    title: 'Rocket Launch — gate, deploy, Platform release deep link',
    summary:
      'Platform STG pipeline run + release gate tags refresh; Open detail navigates to Platform release (Delivery).',
    verifySteps: [
      'Rocket Launch card shows Gate · Deploy DenseTags matching latest bifrost-deliver-platform-stg run.',
      'Signal lamp reflects gate/deploy error vs done states.',
      'Click Open detail → — navigates to Platform release / Delivery (not a dead hash).',
    ],
  },
  {
    id: 'P6-3',
    title: 'Satellite Deploy — env matrix, smoke, Tier B',
    summary:
      'Trade Dev/Stg/Prod reach dots, STG smoke lamp, and Tier B checklist tag align with matrix + smoke probes.',
    verifySteps: [
      'Satellite Deploy card shows Dev · Stg · Prod env dots with reach lamps.',
      'Smoke pass/fail matches GET stg-smoke / gateway :30880 health.',
      'Tier B tag shows signed vs pending from tier-b status API.',
      'Open detail → navigates to Trade Deploy / Satellite delivery surface.',
    ],
  },
  {
    id: 'P6-4',
    title: 'Agent Launch / Deploy + operator auth gate',
    summary:
      'Agent Launch and Agent Deploy buttons dispatch scoped tasks when operator-authenticated; otherwise show auth warning.',
    verifySteps: [
      'Without operator token — Agent buttons disabled and auth warning appears when release/payload degraded.',
      'With operator token — Agent Launch opens release-scope task; Agent Deploy opens trade-deploy scope.',
      'Grafana deep links reachable from Satellite → Telemetry or Cluster observability (Layer B ready).',
    ],
  },
]

export interface ControlRoomPhase6ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase6SignoffState {
  version: string
  items: Record<string, ControlRoomPhase6ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase6_signoff'

function emptyItemState(): ControlRoomPhase6ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase6SignoffState(): ControlRoomPhase6SignoffState {
  const items: Record<string, ControlRoomPhase6ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE6_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE6_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase6SignoffState(): ControlRoomPhase6SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase6SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase6SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE6_VERSION) {
      return defaultPhase6SignoffState()
    }
    const merged = defaultPhase6SignoffState()
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
    return defaultPhase6SignoffState()
  }
}

export function savePhase6SignoffState(state: ControlRoomPhase6SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyControlRoomSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allPhase6ItemsVerified(state: ControlRoomPhase6SignoffState): boolean {
  return CONTROL_ROOM_PHASE6_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase6VerificationCount(state: ControlRoomPhase6SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE6_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE6_DELIVERY_ITEMS.length }
}

export function isControlRoomProgramComplete(): boolean {
  return loadPhase6SignoffState().signedOffAt != null
}
