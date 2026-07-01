/** Control Room Phase 1 delivery checklist — Operate Loop (triangle closure). */

export const CONTROL_ROOM_PHASE1_VERSION = '2026-07-01'

export interface ControlRoomPhase1DeliveryItem {
  id: 'P1-1' | 'P1-2' | 'P1-3' | 'P1-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const CONTROL_ROOM_PHASE1_DELIVERY_ITEMS: ControlRoomPhase1DeliveryItem[] = [
  {
    id: 'P1-1',
    title: 'Diagnose & Fix — compact dispatch pack',
    summary:
      'Degraded mission opens Agent Desk with failing targets + compact ops context, not a one-line prompt.',
    verifySteps: [
      'With Mission CAUTION/CRITICAL, click Diagnose & Fix on Control Room.',
      'Agent Desk composer is prefilled with dispatch brief, failing matrix targets, and compact ops section.',
      'Send the task — runner starts with full context.',
    ],
  },
  {
    id: 'P1-2',
    title: 'Release degraded → Platform release (Agent)',
    summary:
      'When Rocket · Release is not NOMINAL, a one-click Agent dispatch starts scope release.',
    verifySteps: [
      'Expand Program context → Rocket subsystems (or use Release strip CTA when visible).',
      'On degraded Release card, click Platform release (Agent).',
      'Agent Desk opens on a new release-scoped job (or shows operator auth warning if not authenticated).',
    ],
  },
  {
    id: 'P1-3',
    title: 'Mission verify banner after Agent job completes',
    summary:
      'When a running job finishes, cockpit probes refresh and a banner shows Verified NOMINAL or remaining CAUTION.',
    verifySteps: [
      'Start any Agent job from Control Room (Fix or Release).',
      'Wait for job to reach Done or Failed.',
      'Control Room shows verify banner with NOMINAL or still-degraded message; probes refresh automatically.',
    ],
  },
  {
    id: 'P1-4',
    title: 'Active jobs strip — awaiting + recent completed',
    summary:
      'Awaiting approval jobs pulse; recently finished jobs appear muted with Audit link.',
    verifySteps: [
      'Running / Awaiting you jobs show as live chips (awaiting highlighted).',
      'After a job completes, a muted “Recent” chip appears for ~24h.',
      'Audit link in strip header opens actuation history.',
    ],
  },
]

export interface ControlRoomPhase1ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface ControlRoomPhase1SignoffState {
  version: string
  items: Record<string, ControlRoomPhase1ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_control_room_phase1_signoff'

function emptyItemState(): ControlRoomPhase1ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultPhase1SignoffState(): ControlRoomPhase1SignoffState {
  const items: Record<string, ControlRoomPhase1ItemVerification> = {}
  for (const item of CONTROL_ROOM_PHASE1_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: CONTROL_ROOM_PHASE1_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadPhase1SignoffState(): ControlRoomPhase1SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultPhase1SignoffState()
    const parsed = JSON.parse(raw) as ControlRoomPhase1SignoffState
    if (parsed.version !== CONTROL_ROOM_PHASE1_VERSION) {
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

export function savePhase1SignoffState(state: ControlRoomPhase1SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // storage unavailable
  }
}

export function allPhase1ItemsVerified(state: ControlRoomPhase1SignoffState): boolean {
  return CONTROL_ROOM_PHASE1_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function phase1VerificationCount(state: ControlRoomPhase1SignoffState): {
  verified: number
  total: number
} {
  const verified = CONTROL_ROOM_PHASE1_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: CONTROL_ROOM_PHASE1_DELIVERY_ITEMS.length }
}
