import { notifyMissionSignalSignoffChanged } from './missionSignalSignoffEvents'

/** Mission Signal Program — Phase 6 (Flight Director Operations) delivery checklist. */

export const MISSION_SIGNAL_PHASE6_VERSION = '2026-07-02'

export interface MissionSignalPhase6DeliveryItem {
  id: 'MSP6-1' | 'MSP6-2' | 'MSP6-3' | 'MSP6-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS: MissionSignalPhase6DeliveryItem[] = [
  {
    id: 'MSP6-1',
    title: 'Flight Director daily briefing on Agent Briefing',
    summary:
      'Agent Briefing shows 24h Flight Director digest from get_flight_director_snapshot — replaces manual Audit scanning for autonomous job outcomes.',
    verifySteps: [
      'Agent → Agent Briefing — “Flight Director · 24h digest” panel loads without HTTP error.',
      'Digest shows completed / failed / escalations / promotion-pending counts matching snapshot API.',
      'Panel links to Trust & Autonomy for drill-down.',
    ],
  },
  {
    id: 'MSP6-2',
    title: 'Trust override API — Owner actuation levels persisted',
    summary:
      'PUT /api/v1/agent/governance/trust-overrides/{skill_id} persists L0/L1/L2; GET trust-matrix merges owner_overrides.',
    verifySteps: [
      'curl PUT trust-overrides/release with level L0 — returns updated entry with last_override_by.',
      'curl GET trust-matrix — release row shows L0 and data_source includes owner_overrides.',
      'Overrides survive platform-api restart (file-backed store).',
    ],
  },
  {
    id: 'MSP6-3',
    title: 'Trust & Autonomy — adjustable levels + earned autonomy actions',
    summary:
      'Trust Matrix UI: per-skill level selector, Accept promotion / Apply demotion when earned autonomy engine suggests.',
    verifySteps: [
      'Trust & Autonomy — change a skill level via L0/L1/L2 control; matrix refreshes with override timestamp.',
      'Promotion-eligible row shows “Accept promotion” — applies suggested L0.',
      'Demotion-triggered row shows “Apply demotion” — applies suggested L1.',
    ],
  },
  {
    id: 'MSP6-4',
    title: 'Control Room Phase 6 sign-off — flight-director-governance complete',
    summary:
      'Phase 6 panel gated on Mission Signal Phase 5 signed off; completes spine flight-director-governance items ④⑤ (earned autonomy actuation + daily briefing).',
    verifySteps: [
      'Control Room bottom — Mission Signal Phase 6 panel above Phase 5 (PROGRAM COMPLETE).',
      'Live strip shows override count + briefing digest from snapshot.',
      'Sign off Phase 6 — flight-director-governance stream deliverable accepted.',
    ],
  },
]

export interface MissionSignalPhase6ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase6SignoffState {
  version: string
  items: Record<string, MissionSignalPhase6ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase6_signoff'

function emptyItemState(): MissionSignalPhase6ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase6SignoffState(): MissionSignalPhase6SignoffState {
  const items: Record<string, MissionSignalPhase6ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE6_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase6SignoffState(): MissionSignalPhase6SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase6SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase6SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE6_VERSION) {
      return defaultMissionSignalPhase6SignoffState()
    }
    const merged = defaultMissionSignalPhase6SignoffState()
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
    return defaultMissionSignalPhase6SignoffState()
  }
}

export function saveMissionSignalPhase6SignoffState(state: MissionSignalPhase6SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyMissionSignalSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase6ItemsVerified(state: MissionSignalPhase6SignoffState): boolean {
  return MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase6VerificationCount(state: MissionSignalPhase6SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE6_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase6SignedOff(): boolean {
  return loadMissionSignalPhase6SignoffState().signedOffAt != null
}
