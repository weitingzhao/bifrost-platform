import type {
  AgentBridgeResponse,
  ClusterSummary,
  MatrixResponse,
  Reachability,
  RemediationHealthResponse,
  SelfHealthResponse,
  StgSmokeResponse,
  SupplyChainResponse,
  VerifyPayloadResponse,
} from '@/api/types'
import { formatVerifyPayloadGuidance } from '@/lib/control-room/payloadVerification'
import { formatPipelineRunStatus, isPipelineRunFailed } from '@/lib/delivery/pipelineRunAskPack'

export type Signal = Reachability

export interface ModuleState {
  signal: Signal
  value: string
  detail: string
}

export function worst(...signals: Signal[]): Signal {
  if (signals.includes('fail')) return 'fail'
  if (signals.includes('degraded')) return 'degraded'
  if (signals.includes('unknown')) return 'unknown'
  return 'ok'
}

export function signalColor(s: Signal): string {
  switch (s) {
    case 'ok':
      return 'var(--color-lamp-green)'
    case 'degraded':
      return 'var(--color-lamp-yellow)'
    case 'fail':
      return 'var(--color-lamp-red)'
    default:
      return 'var(--muted-foreground)'
  }
}

export type MissionStatus = 'NOMINAL' | 'CAUTION' | 'CRITICAL' | 'PROBING'

export function missionStatus(s: Signal): MissionStatus {
  if (s === 'ok') return 'NOMINAL'
  if (s === 'degraded') return 'CAUTION'
  if (s === 'fail') return 'CRITICAL'
  return 'PROBING'
}

export function missionStatusColor(status: MissionStatus): string {
  switch (status) {
    case 'NOMINAL':
      return 'var(--color-lamp-green)'
    case 'CAUTION':
      return 'var(--color-lamp-yellow)'
    case 'CRITICAL':
      return 'var(--color-lamp-red)'
    default:
      return 'var(--muted-foreground)'
  }
}

/* ── Rocket subsystem signals ── */

export function infraSignal(d?: ClusterSummary): ModuleState {
  if (!d) return { signal: 'unknown', value: '…', detail: 'Cluster: probing' }
  if (d.reachability === 'fail') return { signal: 'fail', value: 'down', detail: 'Cluster API unreachable' }
  if (d.reachability === 'degraded') return { signal: 'degraded', value: 'degraded', detail: `Cluster: ${d.detail}` }
  if (d.failing_pods > 0)
    return { signal: 'degraded', value: `${d.failing_pods} pods`, detail: `${d.failing_pods} failing pods` }
  const standby = d.elastic_standby ?? 0
  return {
    signal: 'ok',
    value: standby > 0 ? `${d.nodes_ready}/${d.nodes_total}+${standby}` : `${d.nodes_ready}/${d.nodes_total}`,
    detail: `Cluster: ${d.nodes_ready}/${d.nodes_total} nodes Ready${standby > 0 ? ` (+${standby} standby)` : ''}`,
  }
}

function stgSmokeCounts(stg?: StgSmokeResponse): { ok: number; total: number; allOk: boolean } {
  const total = stg?.targets.length ?? 0
  const ok = stg?.targets.filter(t => t.reachability === 'ok').length ?? 0
  return { ok, total, allOk: total > 0 && ok === total }
}

function smokeSignalFromCounts(smoke: { ok: number; total: number; allOk: boolean }): Signal {
  if (smoke.total === 0) return 'unknown'
  if (smoke.allOk) return 'ok'
  if (smoke.ok === 0) return 'fail'
  return 'degraded'
}

export function releaseSignal(supply?: SupplyChainResponse, stg?: StgSmokeResponse): ModuleState {
  if (!supply) return { signal: 'unknown', value: '…', detail: 'Release: probing' }
  if (supply.reachability === 'fail') {
    return { signal: 'fail', value: 'down', detail: 'Delivery pipeline unreachable' }
  }
  const last = supply.last_deliver_run
  const smoke = stgSmokeCounts(stg)
  const smokeSignal = smokeSignalFromCounts(smoke)
  const smokeDetail = smoke.total > 0 ? `STG smoke ${smoke.ok}/${smoke.total}` : ''
  if (!last) {
    return {
      signal: worst('ok', smokeSignal),
      value: 'idle',
      detail: smokeDetail !== '' ? `No recent deliver run · ${smokeDetail}` : 'No recent deliver run',
    }
  }
  const pipelineLabel = last.pipeline ?? 'bifrost-deliver-stg'
  const statusLabel = formatPipelineRunStatus(last)
  const running = statusLabel === 'Running' || (last.reason ?? '').toLowerCase() === 'running'
  if (running) {
    return {
      signal: worst('degraded', smokeSignal),
      value: 'shipping',
      detail: `Deliver running: ${pipelineLabel} ${last.name}${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`,
    }
  }
  if (isPipelineRunFailed(last)) {
    if (smoke.allOk) {
      return {
        signal: 'degraded',
        value: 'stale-fail',
        detail: `Deliver-stg failed (${last.name}: ${statusLabel}) · ${smokeDetail} — runtime OK, rerun pipeline (not cluster/nodes)`,
      }
    }
    return {
      signal: worst('fail', smokeSignal),
      value: 'failed',
      detail: `Deliver-stg failed (${last.name}: ${statusLabel})${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`,
    }
  }
  return {
    signal: worst('ok', smokeSignal),
    value: 'shipped',
    detail: `Last deliver: ${pipelineLabel} ${statusLabel}${smokeDetail !== '' ? ` · ${smokeDetail}` : ''}`,
  }
}

export function controlSignal(d?: SelfHealthResponse): ModuleState {
  if (!d) return { signal: 'unknown', value: '…', detail: 'Control plane: probing' }
  const total = d.probes.length
  const ok = d.probes.filter(p => p.status === 'ok').length
  const signal: Signal = d.overall === 'ok' ? 'ok' : d.overall === 'degraded' ? 'degraded' : 'fail'
  return {
    signal,
    value: `${ok}/${total}`,
    detail: `Platform self-health ${ok}/${total} probes OK (platform-api · console · ArgoCD)`,
  }
}

export function agentSignal(runner?: RemediationHealthResponse, bridge?: AgentBridgeResponse): ModuleState {
  // Prefer dual-runner heartbeat from the bridge (Active-Standby HA) when available.
  const runners = bridge?.runners ?? []
  let runnerSig: Signal
  let runnerLabel: string
  if (runners.length >= 2) {
    const upCount = runners.filter(r => r.status === 'ok').length
    if (upCount === runners.length) {
      runnerSig = 'ok'
      runnerLabel = `Runners ${upCount}/${runners.length} (HA)`
    } else if (upCount === 0) {
      runnerSig = 'fail'
      runnerLabel = 'Runners down'
    } else {
      // one down — failover keeps service, but redundancy is lost
      runnerSig = 'degraded'
      const down = runners
        .filter(r => r.status !== 'ok')
        .map(r => r.role ?? r.url)
        .join(', ')
      runnerLabel = `Runner ${down} down — failover active`
    }
  } else if (runners.length === 1) {
    runnerSig = runners[0].status === 'ok' ? 'ok' : 'fail'
    runnerLabel = runnerSig === 'ok' ? 'Runner up (no standby)' : 'Runner down'
  } else {
    runnerSig = runner == null ? 'unknown' : runner.status === 'ok' ? 'ok' : 'fail'
    runnerLabel = runnerSig === 'ok' ? 'Runner up' : runnerSig === 'unknown' ? 'Runner ?' : 'Runner down'
  }

  const gb = bridge?.git_bridge
  const bridgeSig: Signal =
    gb == null
      ? 'unknown'
      : gb.status !== 'ok'
        ? 'fail'
        : (gb.dirty_repos ?? 0) > 0
          ? 'degraded'
          : 'ok'
  const dirty = gb?.dirty_repos ?? 0
  const parts: string[] = [runnerLabel]
  parts.push(
    bridgeSig === 'unknown'
      ? 'Bridge ?'
      : gb?.status !== 'ok'
        ? 'Bridge down'
        : dirty > 0
          ? `Bridge ${dirty} dirty`
          : 'Bridge clean',
  )
  const signal = worst(runnerSig, bridgeSig)
  const runnersUp = runners.filter(r => r.status === 'ok').length
  const value =
    runnerSig === 'fail'
      ? 'down'
      : runners.length >= 2
        ? `${runnersUp}/${runners.length} up`
        : dirty > 0
          ? `${dirty} dirty`
          : signal === 'unknown'
            ? '…'
            : 'ready'
  return { signal, value, detail: `Automation — ${parts.join(' · ')}` }
}

/* ── Payload (Trade satellite) signals ── */

export function tradeEnvSignal(m: MatrixResponse | undefined): ModuleState {
  if (!m) return { signal: 'unknown', value: '…', detail: 'probing' }
  const targets = m.targets.filter(t => t.category.startsWith('trade') || t.category === 'datastore')
  const total = targets.length
  if (total === 0) return { signal: 'unknown', value: 'n/a', detail: 'no targets' }
  const up = targets.filter(t => t.reachability === 'ok').length
  const anyFail = targets.some(t => t.reachability === 'fail')
  const anyDeg = targets.some(t => t.reachability === 'degraded')
  const signal: Signal = anyFail ? 'fail' : anyDeg ? 'degraded' : up === total ? 'ok' : 'degraded'
  return { signal, value: `${up}/${total}`, detail: `${up}/${total} services reachable` }
}

export interface MissionSnapshot {
  infra: ModuleState
  release: ModuleState
  control: ModuleState
  agent: ModuleState
  tradeDev: ModuleState
  tradeProd: ModuleState
  rocketOverall: Signal
  payloadOverall: Signal
  missionOverall: Signal
}

export type MissionDegradationSegment = 'rocket' | 'payload'

export type MissionDegradationItem = {
  segment: MissionDegradationSegment
  id: string
  signal: Signal
  detail: string
}

export function collectRocketDegradationItems(snap: MissionSnapshot): MissionDegradationItem[] {
  const modules: Array<{ id: string; state: ModuleState }> = [
    { id: 'Infra', state: snap.infra },
    { id: 'Release', state: snap.release },
    { id: 'Control', state: snap.control },
    { id: 'Agent', state: snap.agent },
  ]
  return modules
    .filter(m => m.state.signal !== 'ok')
    .map(m => ({
      segment: 'rocket' as const,
      id: m.id,
      signal: m.state.signal,
      detail: m.state.detail,
    }))
}

export function collectPayloadDegradationItems(snap: MissionSnapshot): MissionDegradationItem[] {
  const out: MissionDegradationItem[] = []
  if (snap.tradeDev.signal !== 'ok') {
    out.push({
      segment: 'payload',
      id: 'Trade · dev',
      signal: snap.tradeDev.signal,
      detail: snap.tradeDev.detail,
    })
  }
  if (snap.tradeProd.signal !== 'ok') {
    out.push({
      segment: 'payload',
      id: 'Trade · prod',
      signal: snap.tradeProd.signal,
      detail: snap.tradeProd.detail,
    })
  }
  return out
}

export function collectMissionDegradationItems(snap: MissionSnapshot): MissionDegradationItem[] {
  return [...collectRocketDegradationItems(snap), ...collectPayloadDegradationItems(snap)]
}

export function missionDegradationSummary(
  items: MissionDegradationItem[],
  segment?: MissionDegradationSegment,
): string {
  const filtered = segment != null ? items.filter(i => i.segment === segment) : items
  if (filtered.length === 0) return 'All probes nominal.'
  const critical = filtered.filter(i => i.signal === 'fail').length
  const caution = filtered.filter(i => i.signal === 'degraded').length
  const parts: string[] = []
  if (critical > 0) parts.push(`${critical} critical`)
  if (caution > 0) parts.push(`${caution} caution`)
  return `${filtered.length} issue${filtered.length === 1 ? '' : 's'} (${parts.join(', ')})`
}

/**
 * Generate a structured diagnostic prompt from the current mission snapshot.
 * Returns null when the mission is NOMINAL (nothing to fix).
 */
export function buildDiagnosticPrompt(
  snap: MissionSnapshot,
  verify?: VerifyPayloadResponse,
): string | null {
  if (snap.missionOverall === 'ok') return null

  const lines: string[] = []
  const mission = missionStatus(snap.missionOverall)
  lines.push(`Mission status: ${mission}.`)
  lines.push('', 'Run verify_payload (MCP) or GET /api/v1/mission/verify-payload before fixing datastore targets.')

  const rocketIssues: string[] = []
  const releaseIssues: string[] = []
  const infraIssues: string[] = []
  const modules: Array<{ name: string; state: ModuleState }> = [
    { name: 'Infra', state: snap.infra },
    { name: 'Release', state: snap.release },
    { name: 'Control', state: snap.control },
    { name: 'Agent', state: snap.agent },
  ]
  for (const m of modules) {
    if (m.state.signal === 'ok') continue
    const line = `- ${m.name} (${m.state.signal}): ${m.state.detail}`
    if (m.name === 'Release' || m.state.detail.toLowerCase().includes('deliver')) {
      releaseIssues.push(line)
    } else if (m.name === 'Infra' || m.state.detail.toLowerCase().includes('pod')) {
      infraIssues.push(line)
    } else {
      rocketIssues.push(line)
    }
  }
  if (releaseIssues.length > 0) {
    lines.push('', 'Release / delivery pipeline (Tekton — not a K8s node problem):')
    lines.push(...releaseIssues)
    if (snap.release.value === 'stale-fail') {
      lines.push(
        '- Hint: STG smoke green + deliver-stg failed → use playbook deliver-stg-recover (fix PipelineRun / GitOps, then rerun).',
      )
    }
  }
  if (infraIssues.length > 0) {
    lines.push('', 'Cluster / infra (K8s):')
    lines.push(...infraIssues)
  }
  if (rocketIssues.length > 0) {
    lines.push('', 'Rocket subsystem (control plane):')
    lines.push(...rocketIssues)
  }

  const payloadIssues: string[] = []
  if (snap.tradeDev.signal !== 'ok') payloadIssues.push(`- Trade dev (${snap.tradeDev.signal}): ${snap.tradeDev.detail}`)
  if (snap.tradeProd.signal !== 'ok') payloadIssues.push(`- Trade prod (${snap.tradeProd.signal}): ${snap.tradeProd.detail}`)
  if (payloadIssues.length > 0) {
    lines.push('', 'Payload issues:')
    lines.push(...payloadIssues)
  }

  lines.push('', 'Diagnose root causes for the issues above and propose remediation steps. For read-only checks, proceed automatically. For destructive actions, request operator approval first.')
  lines.push('', 'Before closing the job, call verify_mission_snapshot (MCP) and confirm post_fix_verification.passed is true.')
  lines.push(...formatVerifyPayloadGuidance(verify))
  return lines.join('\n')
}

export function buildMissionSnapshot(input: {
  cluster?: ClusterSummary
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
  self?: SelfHealthResponse
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
  matrices: MatrixResponse[]
}): MissionSnapshot {
  const dev = input.matrices.find(m => m.environment === 'dev')
  const prod = input.matrices.find(m => m.environment === 'prod')
  const infra = infraSignal(input.cluster)
  const release = releaseSignal(input.supply, input.stg)
  const control = controlSignal(input.self)
  const agent = agentSignal(input.runner, input.bridge)
  const tradeDev = tradeEnvSignal(dev)
  const tradeProd = tradeEnvSignal(prod)
  const rocketOverall = worst(infra.signal, release.signal, control.signal, agent.signal)
  const payloadOverall = worst(tradeDev.signal, tradeProd.signal)
  const missionOverall = worst(rocketOverall, payloadOverall)
  return {
    infra,
    release,
    control,
    agent,
    tradeDev,
    tradeProd,
    rocketOverall,
    payloadOverall,
    missionOverall,
  }
}
