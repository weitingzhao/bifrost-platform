import { isMissionSignalPhase1SignedOff } from './missionSignalPhase1Delivery'
import { isMissionSignalPhase2SignedOff } from './missionSignalPhase2Delivery'
import { isMissionSignalPhase3SignedOff } from './missionSignalPhase3Delivery'
import { isMissionSignalPhase4SignedOff } from './missionSignalPhase4Delivery'
import { isMissionSignalPhase5SignedOff } from './missionSignalPhase5Delivery'
import { isMissionSignalPhase6SignedOff } from './missionSignalPhase6Delivery'

export type MissionSignalPhaseId = 'P1' | 'P2' | 'P3' | 'P4' | 'P5' | 'P6'

export type MissionSignalPhaseMeta = {
  id: MissionSignalPhaseId
  shortLabel: string
  signoffLocation: string
}

export const MISSION_SIGNAL_PROGRAM_PHASES: MissionSignalPhaseMeta[] = [
  { id: 'P1', shortLabel: 'Signal Truth', signoffLocation: 'Control Room → Phase 1' },
  { id: 'P2', shortLabel: 'Diagnostic Playbook', signoffLocation: 'Control Room → Phase 2' },
  { id: 'P3', shortLabel: 'Autonomous Loop', signoffLocation: 'Control Room → Phase 3' },
  { id: 'P4', shortLabel: 'Hermes First Task', signoffLocation: 'Control Room → Phase 4' },
  { id: 'P5', shortLabel: 'Flight Director', signoffLocation: 'Control Room → Phase 5' },
  { id: 'P6', shortLabel: 'Flight Director Ops', signoffLocation: 'Control Room → Phase 6' },
]

const SIGNED_OFF: Record<MissionSignalPhaseId, () => boolean> = {
  P1: isMissionSignalPhase1SignedOff,
  P2: isMissionSignalPhase2SignedOff,
  P3: isMissionSignalPhase3SignedOff,
  P4: isMissionSignalPhase4SignedOff,
  P5: isMissionSignalPhase5SignedOff,
  P6: isMissionSignalPhase6SignedOff,
}

export function isMissionSignalPhaseSignedOff(id: MissionSignalPhaseId): boolean {
  return SIGNED_OFF[id]()
}

export function missionSignalProgramSignedCount(): { signed: number; total: number } {
  const signed = MISSION_SIGNAL_PROGRAM_PHASES.filter(p => isMissionSignalPhaseSignedOff(p.id)).length
  return { signed, total: MISSION_SIGNAL_PROGRAM_PHASES.length }
}

export function allMissionSignalPhasesSignedOff(): boolean {
  return MISSION_SIGNAL_PROGRAM_PHASES.every(p => isMissionSignalPhaseSignedOff(p.id))
}

export function priorMissionSignalPhasesSignedOff(): { ok: boolean; missing: string[] } {
  const missing: string[] = []
  for (const p of MISSION_SIGNAL_PROGRAM_PHASES) {
    if (!isMissionSignalPhaseSignedOff(p.id)) {
      missing.push(`${p.id} ${p.shortLabel}`)
    }
  }
  return { ok: missing.length === 0, missing }
}
