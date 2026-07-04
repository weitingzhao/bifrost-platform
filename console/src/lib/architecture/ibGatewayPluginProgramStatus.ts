import { isIbGatewayPluginPhase0SignedOff } from './ibGatewayPluginPhase0Delivery'
import { isIbGatewayPluginPhase1SignedOff } from './ibGatewayPluginPhase1Delivery'
import { isIbGatewayPluginPhase2SignedOff } from './ibGatewayPluginPhase2Delivery'
import { isIbGatewayPluginPhase3SignedOff } from './ibGatewayPluginPhase3Delivery'
import { isIbGatewayPluginPhase4SignedOff } from './ibGatewayPluginPhase4Delivery'

export type IbGatewayPluginProgramPhaseId = 'IBGP0' | 'IBGP1' | 'IBGP2' | 'IBGP3' | 'IBGP4'

export type IbGatewayPluginProgramPhaseMeta = {
  id: IbGatewayPluginProgramPhaseId
  shortLabel: string
  signoffLocation: string
}

export const IB_GATEWAY_PLUGIN_PROGRAM_PHASES: IbGatewayPluginProgramPhaseMeta[] = [
  { id: 'IBGP0', shortLabel: 'redis-ib', signoffLocation: 'Architecture → Plugins → IB Gateway' },
  { id: 'IBGP1', shortLabel: 'Gateway core', signoffLocation: 'Architecture → Plugins → IB Gateway' },
  { id: 'IBGP2', shortLabel: 'Platform API', signoffLocation: 'Architecture → Plugins → IB Gateway' },
  { id: 'IBGP3', shortLabel: 'Trade cutover', signoffLocation: 'Architecture → Plugins → IB Gateway' },
  { id: 'IBGP4', shortLabel: 'Live TWS', signoffLocation: 'Architecture → Plugins → IB Gateway' },
]

const SIGNED_OFF: Partial<Record<IbGatewayPluginProgramPhaseId, () => boolean>> = {
  IBGP0: isIbGatewayPluginPhase0SignedOff,
  IBGP1: isIbGatewayPluginPhase1SignedOff,
  IBGP2: isIbGatewayPluginPhase2SignedOff,
  IBGP3: isIbGatewayPluginPhase3SignedOff,
  IBGP4: isIbGatewayPluginPhase4SignedOff,
}

export function isIbGatewayPluginPhaseSignedOff(id: IbGatewayPluginProgramPhaseId): boolean {
  return SIGNED_OFF[id]?.() ?? false
}

export function ibGatewayPluginProgramSignedCount(): { signed: number; total: number } {
  const signed = IB_GATEWAY_PLUGIN_PROGRAM_PHASES.filter(p => isIbGatewayPluginPhaseSignedOff(p.id)).length
  return { signed, total: IB_GATEWAY_PLUGIN_PROGRAM_PHASES.length }
}

export function allIbGatewayPluginPhasesSignedOff(): boolean {
  return IB_GATEWAY_PLUGIN_PROGRAM_PHASES.every(p => isIbGatewayPluginPhaseSignedOff(p.id))
}
