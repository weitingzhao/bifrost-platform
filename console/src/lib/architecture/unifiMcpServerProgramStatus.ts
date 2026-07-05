import { isUnifiMcpServerPhase1SignedOff } from './unifiMcpServerPhase1Delivery'
import { isUnifiMcpServerPhase2SignedOff } from './unifiMcpServerPhase2Delivery'
import { isUnifiMcpServerPhase3SignedOff } from './unifiMcpServerPhase3Delivery'
import { isUnifiMcpServerPhase4SignedOff } from './unifiMcpServerPhase4Delivery'

export type UnifiMcpServerPhaseId = 'UMS1' | 'UMS2' | 'UMS3' | 'UMS4'

export type UnifiMcpServerPhaseMeta = {
  id: UnifiMcpServerPhaseId
  shortLabel: string
  signoffLocation: string
}

export const UNIFI_MCP_SERVER_PROGRAM_PHASES: UnifiMcpServerPhaseMeta[] = [
  { id: 'UMS1', shortLabel: 'REST client', signoffLocation: 'Subcontractors → Delivery Board · Network Governance · networkApiContractCatalog.ts' },
  { id: 'UMS2', shortLabel: 'MCP read', signoffLocation: 'Subcontractors → Delivery Board · Network Governance · networkApiContractCatalog.ts' },
  { id: 'UMS3', shortLabel: 'Live probe', signoffLocation: 'Mission Control → Control Room → Network Health' },
  { id: 'UMS4', shortLabel: 'MCP write', signoffLocation: 'Subcontractors → Delivery Board · Network Governance · networkApiContractCatalog.ts' },
]

const SIGNED_OFF: Record<UnifiMcpServerPhaseId, () => boolean> = {
  UMS1: isUnifiMcpServerPhase1SignedOff,
  UMS2: isUnifiMcpServerPhase2SignedOff,
  UMS3: isUnifiMcpServerPhase3SignedOff,
  UMS4: isUnifiMcpServerPhase4SignedOff,
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
