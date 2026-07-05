import { isDapProgramSignedOff } from '@/lib/architecture/devAgentPlatformDelivery'
import { isGovernancePhase2SignedOff } from '@/lib/architecture/governancePhase2Delivery'
import { isGovernancePhase7SignedOff } from '@/lib/architecture/governancePhase7Delivery'
import { ibGatewayPluginProgramSignedCount } from '@/lib/architecture/ibGatewayPluginProgramStatus'
import { isIbGatewayPluginHardeningSignedOff } from '@/lib/architecture/ibGatewayPluginHardeningDelivery'
import { isIbGatewayPluginProgramSignedOff } from '@/lib/architecture/ibGatewayPluginProgramDelivery'
import { networkGovernanceProgramSignedCount } from '@/lib/architecture/networkGovernanceProgramStatus'
import { isNetworkGovernancePhase8SignedOff } from '@/lib/architecture/networkGovernancePhase8Delivery'
import { tradeIbClientMigrationProgramSignedCount } from '@/lib/architecture/tradeIbClientMigrationProgramStatus'
import { isTradeIbClientMigrationProgramSignedOff } from '@/lib/architecture/tradeIbClientMigrationProgramDelivery'
import { isTradeIbClientMigrationRolloutDevComposeSignedOff } from '@/lib/architecture/tradeIbClientMigrationRolloutDevComposeDelivery'
import { isTradeIbClientMigrationRolloutProdSignedOff } from '@/lib/architecture/tradeIbClientMigrationRolloutProdDelivery'
import { isTradeIbClientMigrationRolloutStgSignedOff } from '@/lib/architecture/tradeIbClientMigrationRolloutStgDelivery'
import { isTradeIbClientMigrationRolloutW1SignedOff } from '@/lib/architecture/tradeIbClientMigrationRolloutW1Delivery'
import { isTradeIbClientMigrationRolloutW2SignedOff } from '@/lib/architecture/tradeIbClientMigrationRolloutW2Delivery'
import { isTradeIbClientMigrationRolloutW3SignedOff } from '@/lib/architecture/tradeIbClientMigrationRolloutW3Delivery'
import { unifiMcpServerProgramSignedCount } from '@/lib/architecture/unifiMcpServerProgramStatus'
import { loadPhase1SignoffState } from '@/lib/briefing/briefingPhase1Delivery'
import { loadPhase2SignoffState } from '@/lib/briefing/briefingPhase2Delivery'
import { loadPhase3SignoffState } from '@/lib/briefing/briefingPhase3Delivery'
import { loadPhase4SignoffState } from '@/lib/briefing/briefingPhase4Delivery'
import { controlRoomProgramSignedCount } from '@/lib/control-room/controlRoomProgramStatus'
import { isMissionSignalPhase7SignedOff } from '@/lib/control-room/missionSignalPhase7Delivery'
import { missionSignalProgramSignedCount } from '@/lib/control-room/missionSignalProgramStatus'
import { isGovernancePhase1SignedOff } from '@/lib/architecture/governancePhase1Delivery'
import { isGovernancePhase3SignedOff } from '@/lib/architecture/governancePhase3Delivery'
import { isGovernancePhase4SignedOff } from '@/lib/architecture/governancePhase4Delivery'
import { isGovernancePhase5SignedOff } from '@/lib/architecture/governancePhase5Delivery'
import { isGovernancePhase6SignedOff } from '@/lib/architecture/governancePhase6Delivery'

export type DeliveryBoardProgramId =
  | 'mission-signal'
  | 'control-room-ui'
  | 'governance'
  | 'network-governance'
  | 'trade-ib-migration'
  | 'ib-gateway-plugin'
  | 'unifi-mcp'
  | 'briefing'
  | 'dev-agent'
  | 'vision'

export type DeliveryBoardProgramOverview = {
  id: DeliveryBoardProgramId
  label: string
  description: string
  formerLocation: string
  phaseCount: number
  signed: number
  complete: boolean
}

function countSigned(checks: boolean[]): { signed: number; total: number } {
  return { signed: checks.filter(Boolean).length, total: checks.length }
}

function missionSignalSignedCount(): { signed: number; total: number } {
  const base = missionSignalProgramSignedCount()
  const p7 = isMissionSignalPhase7SignedOff()
  return { signed: base.signed + (p7 ? 1 : 0), total: base.total + 1 }
}

function governanceSignedCount(): { signed: number; total: number } {
  return countSigned([
    isGovernancePhase1SignedOff(),
    isGovernancePhase2SignedOff(),
    isGovernancePhase3SignedOff(),
    isGovernancePhase4SignedOff(),
    isGovernancePhase5SignedOff(),
    isGovernancePhase6SignedOff(),
    isGovernancePhase7SignedOff(),
  ])
}

function networkGovernanceSignedCount(): { signed: number; total: number } {
  const base = networkGovernanceProgramSignedCount()
  const ng8 = isNetworkGovernancePhase8SignedOff()
  return { signed: base.signed + (ng8 ? 1 : 0), total: base.total + 1 }
}

function tradeIbMigrationSignedCount(): { signed: number; total: number } {
  const base = tradeIbClientMigrationProgramSignedCount()
  const extra = countSigned([
    isTradeIbClientMigrationProgramSignedOff(),
    isTradeIbClientMigrationRolloutW1SignedOff(),
    isTradeIbClientMigrationRolloutW2SignedOff(),
    isTradeIbClientMigrationRolloutW3SignedOff(),
    isTradeIbClientMigrationRolloutStgSignedOff(),
    isTradeIbClientMigrationRolloutDevComposeSignedOff(),
    isTradeIbClientMigrationRolloutProdSignedOff(),
  ])
  return { signed: base.signed + extra.signed, total: base.total + extra.total }
}

function ibGatewayPluginSignedCount(): { signed: number; total: number } {
  const base = ibGatewayPluginProgramSignedCount()
  const extra = countSigned([
    isIbGatewayPluginProgramSignedOff(),
    isIbGatewayPluginHardeningSignedOff(),
  ])
  return { signed: base.signed + extra.signed, total: base.total + extra.total }
}

function briefingSignedCount(): { signed: number; total: number } {
  return countSigned([
    loadPhase1SignoffState().signedOffAt != null,
    loadPhase2SignoffState().signedOffAt != null,
    loadPhase3SignoffState().signedOffAt != null,
    loadPhase4SignoffState().signedOffAt != null,
  ])
}

export function buildDeliveryBoardProgramOverview(): DeliveryBoardProgramOverview[] {
  const programs: Array<Omit<DeliveryBoardProgramOverview, 'complete'>> = [
    {
      id: 'mission-signal',
      label: 'Mission Signal',
      description: 'Flight Director signal truth → autonomous loop → program closure.',
      formerLocation: 'Mission Control → Control Room',
      ...(() => {
        const { signed, total } = missionSignalSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'control-room-ui',
      label: 'Control Room UI',
      description: 'Console commander layout, payload depth, promote cutover panels.',
      formerLocation: 'Mission Control → Control Room',
      ...(() => {
        const { signed, total } = controlRoomProgramSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'governance',
      label: 'Governance',
      description: 'Constitution, projection, spine semantics, blueprint zones.',
      formerLocation: 'Governance → Blueprint · Governance → Briefing Reconciliation',
      ...(() => {
        const { signed, total } = governanceSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'network-governance',
      label: 'Network Governance',
      description: 'Network constitution, Agent Protocol, Network API program.',
      formerLocation: 'Governance → Blueprint',
      ...(() => {
        const { signed, total } = networkGovernanceSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'trade-ib-migration',
      label: 'Trade IB Migration',
      description: 'Gateway RPC cutover and rollout waves (W1–W3, STG, prod).',
      formerLocation: 'Subcontractors → Plugin Gallery (live) / catalog-only (governance) · Engineer → Briefing / Subcontractors → Delivery Board',
      ...(() => {
        const { signed, total } = tradeIbMigrationSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'ib-gateway-plugin',
      label: 'IB Gateway Plugin',
      description: 'redis-ib plugin phases, program closure, hardening.',
      formerLocation: 'Subcontractors → Plugin Gallery (live) / catalog-only (governance) · Engineer → Briefing / Subcontractors → Delivery Board',
      ...(() => {
        const { signed, total } = ibGatewayPluginSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'unifi-mcp',
      label: 'UniFi MCP Server',
      description: 'REST client, MCP read/write, live probe phases.',
      formerLocation: 'Governance → Standards',
      ...(() => {
        const { signed, total } = unifiMcpServerProgramSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'briefing',
      label: 'Agent Briefing',
      description: 'Briefing workspace phased delivery (P1–P4).',
      formerLocation: 'Engineer → Briefing',
      ...(() => {
        const { signed, total } = briefingSignedCount()
        return { signed, phaseCount: total }
      })(),
    },
    {
      id: 'dev-agent',
      label: 'Dev Agent Platform',
      description: 'Declarative program blueprint and multi-program API.',
      formerLocation: 'Engineer → Dev Agent',
      phaseCount: 1,
      signed: isDapProgramSignedOff() ? 1 : 0,
    },
    {
      id: 'vision',
      label: 'Dual Flywheel Vision',
      description: 'Vision gates V1–V5 (API-backed sign-off).',
      formerLocation: 'Governance → Vision',
      phaseCount: 6,
      signed: 0,
    },
  ]

  return programs.map(p => ({
    ...p,
    complete: p.signed === p.phaseCount && p.phaseCount > 0,
  }))
}
