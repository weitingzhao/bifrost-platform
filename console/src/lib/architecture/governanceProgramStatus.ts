import { isGovernancePhase1SignedOff } from './governancePhase1Delivery'
import { isGovernancePhase2SignedOff } from './governancePhase2Delivery'
import { isGovernancePhase3SignedOff } from './governancePhase3Delivery'
import { isGovernancePhase4SignedOff } from './governancePhase4Delivery'
import { isGovernancePhase5SignedOff } from './governancePhase5Delivery'
import { isGovernancePhase6SignedOff } from './governancePhase6Delivery'

export type GovernancePhaseId = 'P1' | 'P2' | 'P3' | 'P4' | 'P5' | 'P6'

export type GovernancePhaseMeta = {
  id: GovernancePhaseId
  shortLabel: string
  signoffLocation: string
}

export const GOVERNANCE_PROGRAM_PHASES: GovernancePhaseMeta[] = [
  { id: 'P1', shortLabel: 'Constitution', signoffLocation: 'Architecture → Blueprint' },
  { id: 'P2', shortLabel: 'Projection', signoffLocation: 'Operate → Delivery' },
  { id: 'P3', shortLabel: 'Spine semantics', signoffLocation: 'Architecture → Blueprint' },
  { id: 'P4', shortLabel: 'Reconciliation', signoffLocation: 'Agent → Briefing Reconciliation' },
  { id: 'P5', shortLabel: 'Blueprint zones', signoffLocation: 'Architecture → Blueprint' },
  { id: 'P6', shortLabel: 'Catalog cleanup', signoffLocation: 'Agent → Briefing Reconciliation' },
]

const SIGNED_OFF: Record<GovernancePhaseId, () => boolean> = {
  P1: isGovernancePhase1SignedOff,
  P2: isGovernancePhase2SignedOff,
  P3: isGovernancePhase3SignedOff,
  P4: isGovernancePhase4SignedOff,
  P5: isGovernancePhase5SignedOff,
  P6: isGovernancePhase6SignedOff,
}

export function isGovernancePhaseSignedOff(id: GovernancePhaseId): boolean {
  return SIGNED_OFF[id]()
}

export function governanceProgramSignedCount(): { signed: number; total: number } {
  const signed = GOVERNANCE_PROGRAM_PHASES.filter(p => isGovernancePhaseSignedOff(p.id)).length
  return { signed, total: GOVERNANCE_PROGRAM_PHASES.length }
}

export function allGovernancePhasesSignedOff(): boolean {
  return GOVERNANCE_PROGRAM_PHASES.every(p => isGovernancePhaseSignedOff(p.id))
}
