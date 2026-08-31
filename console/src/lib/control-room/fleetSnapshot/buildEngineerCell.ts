/**
 * Engineer Fleet cell (Mac seat / automation).
 */
import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { RemediationHealthResponse } from '@/api/remediationTypes'
import { agentSignal, type Signal } from '@/lib/control-room/missionSignals'
import {
  type FleetCell,
  type FleetStandard,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot/types'
import { signalFromStandards, std } from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey, normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot/nav'

export function buildEngineerCell(input: {
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
  viewerEnv?: FleetViewerEnv
  groundBridgeReady?: boolean
}): FleetCell {
  const state = agentSignal(input.runner, input.bridge)
  const viewerEnv = normalizeViewerEnv(input.viewerEnv)
  const viewerRemote = viewerEnv === 'prod' || viewerEnv === 'stg'
  const probeBridge = input.bridge?.satellite_probe_bridge
  const bridge = input.bridge
  const runners = bridge?.runners ?? []

  let runnerSig: Signal
  let runnerReason: string
  if (runners.length >= 2) {
    const upCount = runners.filter(r => r.status === 'ok').length
    if (upCount === runners.length) {
      runnerSig = 'ok'
      runnerReason = `Runners ${upCount}/${runners.length} (HA)`
    } else if (upCount === 0) {
      runnerSig = 'fail'
      runnerReason = 'All runners down'
    } else {
      runnerSig = 'degraded'
      runnerReason = `Runner failover active (${upCount}/${runners.length} up)`
    }
  } else if (runners.length === 1) {
    runnerSig = runners[0].status === 'ok' ? 'ok' : 'fail'
    runnerReason = runnerSig === 'ok' ? 'Runner up (no standby)' : 'Runner down'
  } else {
    runnerSig = input.runner == null ? 'unknown' : input.runner.status === 'ok' ? 'ok' : 'fail'
    runnerReason =
      runnerSig === 'ok' ? 'Runner up' : runnerSig === 'unknown' ? 'Runner status unknown' : 'Runner down'
  }

  const gb = bridge?.git_bridge
  const dirty = gb?.dirty_repos ?? 0
  // Dirty repos are informational (Owner WIP), not a degradation signal.
  // Only bridge unreachable/down is a real failure.
  const gitSig: Signal =
    gb == null ? 'unknown' : gb.status !== 'ok' ? 'fail' : 'ok'
  const gitReason =
    gb == null
      ? 'Git bridge status unknown'
      : gb.status !== 'ok'
        ? 'Git bridge down'
        : dirty > 0
          ? `Git bridge OK · ${dirty} dirty repo(s)`
          : 'Git bridge clean'

  let macSig: Signal = 'unknown'
  let macReason = 'Mac seat: probing'
  if (viewerRemote) {
    macSig =
      probeBridge == null
        ? 'ok'
        : probeBridge.status === 'ok'
          ? 'ok'
          : (probeBridge.status as Signal) === 'degraded'
            ? 'degraded'
            : 'fail'
    macReason =
      probeBridge == null
        ? 'Mac seat N/A from this viewer (info only)'
        : `Mac seat · probe-bridge ${probeBridge.status} (info only from remote)`
  } else if (probeBridge != null) {
    macSig =
      probeBridge.status === 'ok' ? 'ok' : probeBridge.status === 'degraded' ? 'degraded' : 'fail'
    macReason =
      probeBridge.status === 'ok'
        ? 'Mac seat · probe-bridge ok'
        : `Mac seat · probe-bridge ${probeBridge.status}${
            probeBridge.error ? `: ${probeBridge.error}` : ''
          }`
  } else if (input.groundBridgeReady === false) {
    macSig = 'degraded'
    macReason = 'Mac seat · probe-bridge not ready'
  }

  const standards: FleetStandard[] = [
    std('runners', 'Agent runners (HA)', runnerSig, runnerReason, 'automation'),
    std('git-bridge', 'Git bridge clean', gitSig, gitReason, 'automation'),
    std('mac-seat', 'Mac seat · probe-bridge', macSig, macReason, 'seat', !viewerRemote),
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

  // Agent Fix only when at least one runner can execute (bridge-down is auto-fixable via bdev).
  const runnersCanAct = runnerSig === 'ok' || runnerSig === 'degraded'
  const canAgentFix = runnersCanAct && signal !== 'ok' && signal !== 'unknown'

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
    agentFixDisabledReason: !runnersCanAct
      ? 'Runners down — recover remediation runners on Operator Plane before Agent Fix'
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
