import { isNetworkGovernancePhase1SignedOff } from './networkGovernancePhase1Delivery'
import { isNetworkGovernancePhase2SignedOff } from './networkGovernancePhase2Delivery'
import { isNetworkGovernancePhase3SignedOff } from './networkGovernancePhase3Delivery'
import { isNetworkGovernancePhase4SignedOff } from './networkGovernancePhase4Delivery'
import { isNetworkGovernancePhase5SignedOff } from './networkGovernancePhase5Delivery'
import { isNetworkGovernancePhase6SignedOff } from './networkGovernancePhase6Delivery'
import { isNetworkGovernancePhase7SignedOff } from './networkGovernancePhase7Delivery'

export type NetworkGovernancePhaseId = 'NG1' | 'NG2' | 'NG3' | 'NG4' | 'NG5' | 'NG6' | 'NG7'

export type NetworkGovernancePhaseMeta = {
  id: NetworkGovernancePhaseId
  shortLabel: string
  signoffLocation: string
}

export const NETWORK_GOVERNANCE_PROGRAM_PHASES: NetworkGovernancePhaseMeta[] = [
  { id: 'NG1', shortLabel: 'Constitution', signoffLocation: 'Architecture → Blueprint' },
  { id: 'NG2', shortLabel: 'AI capabilities', signoffLocation: 'Architecture → Blueprint' },
  { id: 'NG3', shortLabel: 'Spine', signoffLocation: 'Architecture → Blueprint' },
  { id: 'NG4', shortLabel: 'Agent Protocol', signoffLocation: 'Architecture → Blueprint' },
  { id: 'NG5', shortLabel: 'Network Upgrade', signoffLocation: 'Architecture → Blueprint' },
  { id: 'NG6', shortLabel: 'Control Room', signoffLocation: 'Architecture → Blueprint' },
  { id: 'NG7', shortLabel: 'Network API', signoffLocation: 'Architecture → Blueprint' },
]

const SIGNED_OFF: Record<NetworkGovernancePhaseId, () => boolean> = {
  NG1: isNetworkGovernancePhase1SignedOff,
  NG2: isNetworkGovernancePhase2SignedOff,
  NG3: isNetworkGovernancePhase3SignedOff,
  NG4: isNetworkGovernancePhase4SignedOff,
  NG5: isNetworkGovernancePhase5SignedOff,
  NG6: isNetworkGovernancePhase6SignedOff,
  NG7: isNetworkGovernancePhase7SignedOff,
}

export function isNetworkGovernancePhaseSignedOff(id: NetworkGovernancePhaseId): boolean {
  return SIGNED_OFF[id]()
}

export function networkGovernanceProgramSignedCount(): { signed: number; total: number } {
  const signed = NETWORK_GOVERNANCE_PROGRAM_PHASES.filter(p => isNetworkGovernancePhaseSignedOff(p.id)).length
  return { signed, total: NETWORK_GOVERNANCE_PROGRAM_PHASES.length }
}

export function allNetworkGovernancePhasesSignedOff(): boolean {
  return NETWORK_GOVERNANCE_PROGRAM_PHASES.every(p => isNetworkGovernancePhaseSignedOff(p.id))
}
