import { isControlRoomPhase0SignedOff } from './controlRoomPhase0Delivery'
import { loadPhase1SignoffState } from './controlRoomPhase1Delivery'
import { loadPhase2SignoffState } from './controlRoomPhase2Delivery'
import { loadPhase3SignoffState } from './controlRoomPhase3Delivery'
import { loadPhase4SignoffState } from './controlRoomPhase4Delivery'
import { loadPhase5SignoffState } from './controlRoomPhase5Delivery'

export type ControlRoomPhaseId = 'P0' | 'P1' | 'P2' | 'P3' | 'P4' | 'P5'

export type ControlRoomPhaseMeta = {
  id: ControlRoomPhaseId
  shortLabel: string
  signoffLocation: string
}

export const CONTROL_ROOM_PROGRAM_PHASES: ControlRoomPhaseMeta[] = [
  { id: 'P0', shortLabel: 'Structure', signoffLocation: 'Control Room → Phase 0' },
  { id: 'P1', shortLabel: 'Operate Loop', signoffLocation: 'Control Room → Phase 1' },
  { id: 'P2', shortLabel: 'Payload depth', signoffLocation: 'Control Room → Phase 2' },
  { id: 'P3', shortLabel: 'Command intent', signoffLocation: 'Control Room → Phase 3' },
  { id: 'P4', shortLabel: 'Mission timeline', signoffLocation: 'Control Room → Phase 4' },
  { id: 'P5', shortLabel: 'Promote / cutover', signoffLocation: 'Control Room → Phase 5' },
]

const SIGNED_OFF: Record<ControlRoomPhaseId, () => boolean> = {
  P0: isControlRoomPhase0SignedOff,
  P1: () => loadPhase1SignoffState().signedOffAt != null,
  P2: () => loadPhase2SignoffState().signedOffAt != null,
  P3: () => loadPhase3SignoffState().signedOffAt != null,
  P4: () => loadPhase4SignoffState().signedOffAt != null,
  P5: () => loadPhase5SignoffState().signedOffAt != null,
}

export function isControlRoomPhaseSignedOff(id: ControlRoomPhaseId): boolean {
  return SIGNED_OFF[id]()
}

export function controlRoomProgramSignedCount(): { signed: number; total: number } {
  const signed = CONTROL_ROOM_PROGRAM_PHASES.filter(p => isControlRoomPhaseSignedOff(p.id)).length
  return { signed, total: CONTROL_ROOM_PROGRAM_PHASES.length }
}

export { isControlRoomPhase0SignedOff }
