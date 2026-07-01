/** Mission Signal Program — Phase 7 (Program closure) delivery checklist. */

import {
  MISSION_SIGNAL_PROGRAM_PHASES,
  isMissionSignalPhaseSignedOff,
} from './missionSignalProgramStatus'
import { notifyMissionSignalSignoffChanged } from './missionSignalSignoffEvents'

export const MISSION_SIGNAL_PHASE7_VERSION = '2026-07-02'

export interface MissionSignalPhase7DeliveryItem {
  id: 'MSP7-1' | 'MSP7-2' | 'MSP7-3' | 'MSP7-4'
  title: string
  summary: string
  verifySteps: string[]
}

export const MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS: MissionSignalPhase7DeliveryItem[] = [
  {
    id: 'MSP7-1',
    title: 'Mission Signal program status strip',
    summary:
      'Control Room shows P1–P6 sign-off progress — each tag ✓ when Owner signed that phase panel; all six unlock Phase 7 closure.',
    verifySteps: [
      'Control Room — “Mission Signal program” strip shows P1–P6 tags.',
      'Each tag shows ✓ when that phase is signed off in its panel below.',
      'When all six signed, strip reads “All phases signed — ready for Phase 7 program closure”.',
    ],
  },
  {
    id: 'MSP7-2',
    title: 'Agent Protocol — full Mission Signal arc',
    summary:
      'Agent Protocol documents Phases 1–6 playbooks plus Phase 7 program closure — single doctrine reference for Agent modes.',
    verifySteps: [
      'Agent → Agent Protocol — sections for Phases 1–6 (Signal Truth through Flight Director operations).',
      '“Mission Signal Program closure (Phase 7)” section lists maintenance-mode expectations.',
      'Copy pack / LLM appendix includes Mission Signal closure paragraph.',
    ],
  },
  {
    id: 'MSP7-3',
    title: 'Six phase panels reachable and signed',
    summary:
      'Owner can scroll Control Room bottom and confirm Phases 1–6 each show SIGNED / COMPLETE before program closure.',
    verifySteps: [
      'Control Room bottom — Phase 6 FLIGHT DIRECTOR COMPLETE above Phase 5 PROGRAM COMPLETE.',
      'Phases 4–1 panels show SIGNED with operator timestamp.',
      'Phase 7 panel gated until Phase 6 signed off.',
    ],
  },
  {
    id: 'MSP7-4',
    title: 'Mission Signal Program closure sign-off',
    summary:
      'Owner accepts Phases 1–7 delivery as complete — Mission Signal enters maintenance mode; future work is event-driven patches.',
    verifySteps: [
      'All P1–P6 phase panels signed before enabling Phase 7 sign-off.',
      'Mark MSP7-1–3 verified, then Sign off Phase 7.',
      'Panel shows MISSION SIGNAL PROGRAM COMPLETE.',
    ],
  },
]

export interface MissionSignalPhase7ItemVerification {
  verified: boolean
  verifiedAt: string | null
}

export interface MissionSignalPhase7SignoffState {
  version: string
  items: Record<string, MissionSignalPhase7ItemVerification>
  signedOffAt: string | null
  signedOffBy: string | null
  note: string | null
}

const STORAGE_KEY = 'bifrost_mission_signal_phase7_signoff'

function emptyItemState(): MissionSignalPhase7ItemVerification {
  return { verified: false, verifiedAt: null }
}

export function defaultMissionSignalPhase7SignoffState(): MissionSignalPhase7SignoffState {
  const items: Record<string, MissionSignalPhase7ItemVerification> = {}
  for (const item of MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS) {
    items[item.id] = emptyItemState()
  }
  return {
    version: MISSION_SIGNAL_PHASE7_VERSION,
    items,
    signedOffAt: null,
    signedOffBy: null,
    note: null,
  }
}

export function loadMissionSignalPhase7SignoffState(): MissionSignalPhase7SignoffState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw == null) return defaultMissionSignalPhase7SignoffState()
    const parsed = JSON.parse(raw) as MissionSignalPhase7SignoffState
    if (parsed.version !== MISSION_SIGNAL_PHASE7_VERSION) {
      return defaultMissionSignalPhase7SignoffState()
    }
    const merged = defaultMissionSignalPhase7SignoffState()
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
    return defaultMissionSignalPhase7SignoffState()
  }
}

export function saveMissionSignalPhase7SignoffState(state: MissionSignalPhase7SignoffState): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    notifyMissionSignalSignoffChanged()
  } catch {
    // storage unavailable
  }
}

export function allMissionSignalPhase7ItemsVerified(state: MissionSignalPhase7SignoffState): boolean {
  return MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS.every(item => state.items[item.id]?.verified === true)
}

export function missionSignalPhase7VerificationCount(state: MissionSignalPhase7SignoffState): {
  verified: number
  total: number
} {
  const verified = MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS.filter(
    item => state.items[item.id]?.verified === true,
  ).length
  return { verified, total: MISSION_SIGNAL_PHASE7_DELIVERY_ITEMS.length }
}

export function isMissionSignalPhase7SignedOff(): boolean {
  return loadMissionSignalPhase7SignoffState().signedOffAt != null
}

export function priorMissionSignalProgramPhasesSignedOff(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  for (const p of MISSION_SIGNAL_PROGRAM_PHASES) {
    if (!isMissionSignalPhaseSignedOff(p.id)) {
      missing.push(`${p.id} ${p.shortLabel}`)
    }
  }
  return { ok: missing.length === 0, missing }
}
