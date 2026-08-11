import type { IbGatewayStatusResponse, MarketDataStatusResponse } from '@/api/satelliteBusTypes'
import type { Signal } from '@/lib/control-room/missionSignals'
import type { PluginLaunchEvidence, PluginLaunchTargetId } from '@/lib/delivery/pluginLaunchEvidence'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'
import { launchVerdictToSignal } from '@/lib/task-mode/satelliteLaunchVerdict'

export type ResolvePluginLaunchVerdictInput = {
  canOperate: boolean
  target?: PluginLaunchTargetId
  status?: IbGatewayStatusResponse
  marketDataStatus?: MarketDataStatusResponse
  evidence?: PluginLaunchEvidence
  agentInFlight?: boolean
}

export function resolvePluginLaunchVerdict(input: ResolvePluginLaunchVerdictInput): LaunchVerdict {
  if (input.agentInFlight) {
    return {
      kind: 'IN_FLIGHT',
      title: 'Plugin launch in flight',
      detail: 'AI Launch Plugin agent is running — approvals in Operator Dock.',
      disabledReason: 'Plugin launch agent already running',
    }
  }

  if (!input.canOperate) {
    return {
      kind: 'NO_GO',
      title: 'Authenticate to launch',
      detail: 'Operator token required for Launch Plugin.',
      disabledReason: 'Authenticate as operator to launch plugin',
      blockKind: 'auth',
    }
  }

  const target = input.target ?? 'ib-gateway'
  const verifyOk = input.evidence?.verifyOutcome === 'ok'
  const installOk = input.evidence?.installOutcome === 'ok'

  if (target === 'market-data') {
    const md = input.marketDataStatus
    const reach = md?.reachability ?? (md?.reachable === true ? 'ok' : md?.reachable === false ? 'fail' : '')
    if (reach === 'fail') {
      return {
        kind: 'NO_GO',
        title: 'Market Data unreachable',
        detail: md?.summary ?? 'Plugin Market Data probe failed — open Detect step or Market Data manage.',
        disabledReason: 'Fix Market Data reachability before publish',
      }
    }
    if (verifyOk) {
      return {
        kind: 'GO',
        title: 'Ready to (re)publish Market Data',
        detail: 'Last verify ok. AI Launch Plugin republishes via kubectl apply.',
      }
    }
    if (installOk && !verifyOk) {
      return {
        kind: 'GO',
        title: 'Apply recorded — verify next',
        detail: 'Install evidence present; run seat verify (or AI Launch Plugin to continue).',
      }
    }
    return {
      kind: 'GO',
      title: 'Ready for Launch Plugin',
      detail: 'Market Data · Detect → Approve → Install → Verify → Live. Gallery/manage ≠ Publish.',
    }
  }

  const mode = input.status?.mode ?? ''
  const ready = input.status?.deployment?.ready ?? ''
  const reach = input.status?.reachability ?? (input.status?.reachable === true ? 'ok' : '')

  if (verifyOk && (mode === 'live' || mode === 'mock') && (reach === 'ok' || reach === 'degraded')) {
    return {
      kind: 'GO',
      title: 'Ready to (re)publish plugin',
      detail: `mode ${mode || '—'} · deploy ${ready || '—'} · last verify ok. AI Launch Plugin republishes via make install.`,
    }
  }

  if (installOk && !verifyOk) {
    return {
      kind: 'GO',
      title: 'Install recorded — verify next',
      detail: 'Install evidence present; run verify-ib-gateway-program (or AI Launch Plugin to continue).',
    }
  }

  if (reach === 'fail') {
    return {
      kind: 'NO_GO',
      title: 'IB Gateway unreachable',
      detail: input.status?.summary ?? 'Plugin bus probe failed — open Launch Plugin Detect step.',
      disabledReason: 'Fix IB Gateway reachability before publish',
    }
  }

  // Default: allow Agent Launch when authenticated — Detect step will gather state.
  return {
    kind: 'GO',
    title: 'Ready for Launch Plugin',
    detail: `mode ${mode || 'unknown'} · deploy ${ready || '—'} · Detect → Approve → Install → Verify → Live.`,
  }
}

export function buildPluginLaunchCheckpoints(input: {
  canOperate: boolean
  target?: PluginLaunchTargetId
  status?: IbGatewayStatusResponse
  marketDataStatus?: MarketDataStatusResponse
  evidence?: PluginLaunchEvidence
  agentInFlight?: boolean
}): LaunchCheckpoint[] {
  const target = input.target ?? 'ib-gateway'
  const agentCp: LaunchCheckpoint = {
    id: 'readiness',
    label: 'Agent',
    ok: !input.agentInFlight,
    signal: input.agentInFlight ? 'degraded' : 'ok',
    detail: input.agentInFlight ? 'In flight' : 'Idle',
  }
  const authCp: LaunchCheckpoint = {
    id: 'auth',
    label: 'Operator auth',
    ok: input.canOperate,
    signal: input.canOperate ? 'ok' : 'fail',
    detail: input.canOperate ? 'can_operate' : 'Authenticate',
  }
  const verifyCp: LaunchCheckpoint = {
    id: 'pipeline',
    label: 'Last verify',
    ok: input.evidence?.verifyOutcome === 'ok',
    signal:
      input.evidence?.verifyOutcome === 'ok'
        ? 'ok'
        : input.evidence?.verifyOutcome === 'failed'
          ? 'fail'
          : 'unknown',
    detail: input.evidence?.lastVerifyAt
      ? `Verify ${input.evidence.verifyOutcome} @ ${new Date(input.evidence.lastVerifyAt).toLocaleString()}`
      : 'No verify evidence yet',
    readinessAnchor: 'pipeline',
  }

  if (target === 'market-data') {
    const md = input.marketDataStatus
    const reach = md?.reachability
    const reachOk = reach === 'ok' || reach === 'degraded' || md?.reachable === true
    return [
      authCp,
      {
        id: 'rocket',
        label: 'Market Data bus',
        ok: reachOk,
        signal: (reach as Signal | undefined) ?? (reachOk ? 'ok' : 'unknown'),
        detail: md?.summary ?? 'Probe market-data',
        readinessAnchor: 'rocket',
      },
      verifyCp,
      {
        id: 'promote',
        label: 'D10',
        ok: true,
        signal: 'ok',
        detail: 'Polygon REST only — no place_order',
      },
      agentCp,
    ]
  }

  const mode = input.status?.mode ?? ''
  const reach = input.status?.reachability
  const reachOk = reach === 'ok' || reach === 'degraded' || input.status?.reachable === true
  return [
    authCp,
    {
      id: 'rocket',
      label: 'Plugin bus',
      ok: reachOk,
      signal: (reach as Signal | undefined) ?? (reachOk ? 'ok' : 'unknown'),
      detail: input.status?.summary ?? 'Probe ib-gateway',
      readinessAnchor: 'rocket',
    },
    verifyCp,
    {
      id: 'promote',
      label: 'Mode',
      ok: mode === 'live' || mode === 'mock',
      signal: mode === 'live' ? 'ok' : mode === 'mock' ? 'degraded' : 'unknown',
      detail: mode !== '' ? `mode ${mode}` : 'mode unknown',
    },
    agentCp,
  ]
}

export { launchVerdictToSignal }
