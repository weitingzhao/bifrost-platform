import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { ClusterSummary } from '@/api/clusterTypes'
import type { Reachability, SelfHealthResponse } from '@/api/matrixTypes'
import { bridgeRunners } from '@/lib/agent/macHostRole'

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

export type MissionStatus = 'NOMINAL' | 'CAUTION' | 'CRITICAL' | 'PROBING'

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

export function agentSignal(bridge?: AgentBridgeResponse): ModuleState {
  const probes = bridgeRunners(bridge)
  let planeSig: Signal
  let planeLabel: string
  if (probes.length >= 2) {
    const upCount = probes.filter(r => r.status === 'ok').length
    if (upCount === probes.length) {
      planeSig = 'ok'
      planeLabel = `operator-plane ${upCount}/${probes.length}`
    } else if (upCount === 0) {
      planeSig = 'fail'
      planeLabel = 'operator-plane down'
    } else {
      planeSig = 'degraded'
      const down = probes
        .filter(r => r.status !== 'ok')
        .map(r => r.role ?? r.url)
        .join(', ')
      planeLabel = `operator-plane ${down} down`
    }
  } else if (probes.length === 1) {
    planeSig = probes[0].status === 'ok' ? 'ok' : 'fail'
    planeLabel = planeSig === 'ok' ? 'operator-plane up' : 'operator-plane down'
  } else {
    planeSig = 'unknown'
    planeLabel = 'operator-plane ?'
  }

  // git-bridge is a laptop tool (ADR §4): PROD reaching it, or not, says nothing
  // about the operator plane, so it does not feed this signal.
  const signal = planeSig
  const planesUp = probes.filter(r => r.status === 'ok').length
  const value =
    planeSig === 'fail'
      ? 'down'
      : probes.length >= 2
        ? `${planesUp}/${probes.length} up`
        : signal === 'unknown'
          ? '…'
          : 'ready'
  return { signal, value, detail: `Automation — ${planeLabel}` }
}
