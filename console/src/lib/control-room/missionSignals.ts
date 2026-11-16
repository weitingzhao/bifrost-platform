import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse, SelfHealthResponse } from '@/api/matrixTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import '@/lib/control-room/payloadVerification'
import { tradeReadinessTargets } from '@/lib/control-room/matrixSummary'

import type { Signal, ModuleState } from '@/domains/foundation/observation-display/signals'
import { worst, infraSignal, controlSignal, agentSignal } from '@/domains/foundation/observation-display/signals'
export { worst, infraSignal, controlSignal, agentSignal } from '@/domains/foundation/observation-display/signals'
export type { Signal, ModuleState, MissionStatus } from '@/domains/foundation/observation-display/signals'
import { releaseSignal } from '@/domains/delivery/runtime-observation/releaseSignal'

/* ── Rocket subsystem signals ── */







/* ── Payload (Trade satellite) signals ── */

export function tradeEnvSignal(m: MatrixResponse | undefined): ModuleState {
  if (!m) return { signal: 'unknown', value: '…', detail: 'probing' }
  // Exclude policy-blocked write stubs and optional skipped ops-capabilities — they must not
  // degrade Satellite launch Go/No-Go when real HTTP/datastore probes are healthy.
  const targets = tradeReadinessTargets(m.targets)
  const total = targets.length
  if (total === 0) return { signal: 'unknown', value: 'n/a', detail: 'no scored targets' }
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
  /** STG trade matrix (must not be dropped from payload / mission overall). */
  tradeStg: ModuleState
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

function collectRocketDegradationItems(snap: MissionSnapshot): MissionDegradationItem[] {
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

function collectPayloadDegradationItems(snap: MissionSnapshot): MissionDegradationItem[] {
  const out: MissionDegradationItem[] = []
  if (snap.tradeDev.signal !== 'ok') {
    out.push({
      segment: 'payload',
      id: 'Trade · dev',
      signal: snap.tradeDev.signal,
      detail: snap.tradeDev.detail,
    })
  }
  if (snap.tradeStg.signal !== 'ok') {
    out.push({
      segment: 'payload',
      id: 'Trade · stg',
      signal: snap.tradeStg.signal,
      detail: snap.tradeStg.detail,
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

export function buildMissionSnapshot(input: {
  cluster?: ClusterSummary
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
  self?: SelfHealthResponse
  bridge?: AgentBridgeResponse
  matrices: MatrixResponse[]
}): MissionSnapshot {
  const dev = input.matrices.find(m => m.environment === 'dev')
  const stgMatrix = input.matrices.find(m => m.environment === 'stg')
  const prod = input.matrices.find(m => m.environment === 'prod')
  const infra = infraSignal(input.cluster)
  const release = releaseSignal(input.supply, input.stg)
  const control = controlSignal(input.self)
  const agent = agentSignal(input.bridge)
  const tradeDev = tradeEnvSignal(dev)
  const tradeStg = stgMatrix
    ? tradeEnvSignal(stgMatrix)
    : input.stg && input.stg.targets.length > 0
      ? (() => {
          const ok = input.stg.targets.filter(t => t.reachability === 'ok').length
          const total = input.stg.targets.length
          const anyFail = input.stg.targets.some(t => t.reachability === 'fail')
          const anyDeg = input.stg.targets.some(t => t.reachability === 'degraded')
          const signal: Signal = anyFail ? 'fail' : anyDeg ? 'degraded' : ok === total ? 'ok' : 'degraded'
          return { signal, value: `${ok}/${total}`, detail: `STG smoke ${ok}/${total} targets` }
        })()
      : { signal: 'unknown' as Signal, value: 'n/a', detail: 'No STG matrix or smoke' }
  const tradeProd = tradeEnvSignal(prod)
  const rocketOverall = worst(infra.signal, release.signal, control.signal, agent.signal)
  const payloadOverall = worst(tradeDev.signal, tradeStg.signal, tradeProd.signal)
  const missionOverall = worst(rocketOverall, payloadOverall)
  return {
    infra,
    release,
    control,
    agent,
    tradeDev,
    tradeStg,
    tradeProd,
    rocketOverall,
    payloadOverall,
    missionOverall,
  }
}
