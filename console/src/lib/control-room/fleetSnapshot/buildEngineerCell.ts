/**
 * Engineer Fleet cell (operator-plane automation).
 */
import type { AgentBridgeResponse } from '@/api/agentTypes'
import { bridgeRunners } from '@/lib/agent/macHostRole'
import { agentSignal, type Signal } from '@/lib/control-room/missionSignals'
import {
  type FleetCell,
  type FleetStandard,
} from '@/lib/control-room/fleetSnapshot/types'
import { signalFromStandards, std } from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey } from '@/lib/control-room/fleetSnapshot/nav'

export function buildEngineerCell(input: {
  bridge?: AgentBridgeResponse
}): FleetCell {
  const state = agentSignal(input.bridge)
  const bridge = input.bridge
  const probes = bridgeRunners(bridge)

  let planeSig: Signal
  let planeReason: string
  if (probes.length >= 2) {
    const upCount = probes.filter(r => r.status === 'ok').length
    if (upCount === probes.length) {
      planeSig = 'ok'
      planeReason = `operator-plane ${upCount}/${probes.length}`
    } else if (upCount === 0) {
      planeSig = 'fail'
      planeReason = 'operator-plane down'
    } else {
      planeSig = 'degraded'
      planeReason = `operator-plane ${upCount}/${probes.length} up`
    }
  } else if (probes.length === 1) {
    planeSig = probes[0].status === 'ok' ? 'ok' : 'fail'
    planeReason = planeSig === 'ok' ? 'operator-plane up' : 'operator-plane down'
  } else {
    planeSig = 'unknown'
    planeReason = 'operator-plane status unknown'
  }

  const standards: FleetStandard[] = [
    std('runners', 'operator-plane', planeSig, planeReason, 'automation'),
  ]
  const signal = signalFromStandards(standards)
  const critical = signal === 'fail'
  const value =
    signal === 'ok'
      ? state.value
      : signal === 'fail'
        ? 'down'
        : signal === 'degraded'
          ? 'drift'
          : state.value

  const planeCanAct = planeSig === 'ok' || planeSig === 'degraded'
  const canAgentFix = planeCanAct && signal !== 'ok' && signal !== 'unknown'

  return {
    key: cellKey('engineer', 'span'),
    role: 'engineer',
    env: null,
    span: true,
    signal,
    value,
    detail: standards.map(s => s.reason).join(' · '),
    probePath: '',
    standards,
    fixScope: canAgentFix ? 'operator-plane-remediate' : null,
    agentFixEnabled: canAgentFix,
    agentFixDisabledReason: !planeCanAct
      ? 'operator-plane down — recover operator-plane before Agent Fix'
      : signal === 'ok'
        ? undefined
        : signal === 'unknown'
          ? 'Still probing'
          : undefined,
    escalateTabId: critical || signal === 'degraded' ? 'operator-plane' : undefined,
    countsTowardVerdict: true,
  }
}

/** Ground = cluster / Operator Plane infrastructure — Mac seat belongs to Engineer. */
