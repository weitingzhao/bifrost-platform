import { isUnifiMcpServerPhase1SignedOff } from './unifiMcpServerPhase1Delivery'

export type UnifiMcpServerPhaseId = 'UMS1' | 'UMS2' | 'UMS3' | 'UMS4'

export type UnifiMcpServerPhaseMeta = {
  id: UnifiMcpServerPhaseId
  shortLabel: string
  signoffLocation: string
}

export const UNIFI_MCP_SERVER_PROGRAM_PHASES: UnifiMcpServerPhaseMeta[] = [
  { id: 'UMS1', shortLabel: 'REST client', signoffLocation: 'Architecture → Network API' },
  { id: 'UMS2', shortLabel: 'MCP read', signoffLocation: 'Architecture → Network API' },
  { id: 'UMS3', shortLabel: 'Live probe', signoffLocation: 'Architecture → Network API' },
  { id: 'UMS4', shortLabel: 'MCP write', signoffLocation: 'Architecture → Network API' },
]

const SIGNED_OFF: Record<UnifiMcpServerPhaseId, () => boolean> = {
  UMS1: isUnifiMcpServerPhase1SignedOff,
  UMS2: () => false,
  UMS3: () => false,
  UMS4: () => false,
}

export function isUnifiMcpServerPhaseSignedOff(id: UnifiMcpServerPhaseId): boolean {
  return SIGNED_OFF[id]()
}

export function unifiMcpServerProgramSignedCount(): { signed: number; total: number } {
  const signed = UNIFI_MCP_SERVER_PROGRAM_PHASES.filter(p => isUnifiMcpServerPhaseSignedOff(p.id)).length
  return { signed, total: UNIFI_MCP_SERVER_PROGRAM_PHASES.length }
}

export function allUnifiMcpServerPhasesSignedOff(): boolean {
  return UNIFI_MCP_SERVER_PROGRAM_PHASES.every(p => isUnifiMcpServerPhaseSignedOff(p.id))
}
