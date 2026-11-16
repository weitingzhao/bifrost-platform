import type { TelemetryMetricResult } from '@/api/clusterTypes'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { BusHealthInput } from '@/domains/foundation/observability/observabilityViewModel'
import type { EvaluatedSignal, ObservabilityEnvId, SignalState, GoldenSignalRow } from '@/domains/foundation/observability/types'
import { getSignalDef } from '@/domains/foundation/observability/signalRegistry'
function metricById(metrics: TelemetryMetricResult[] | null | undefined, id: string) { return metrics?.find(m => m.id === id) }

export function evaluateBus(bus: BusHealthInput | null | undefined, env: ObservabilityEnvId): EvaluatedSignal {
  const def = getSignalDef('satellite.bus-health')!
  if (bus == null) {
    return { def, state: 'unknown', summary: 'Bus probe not provided', env }
  }
  const state: SignalState =
    bus.health === 'healthy'
      ? 'healthy'
      : bus.health === 'degraded'
        ? 'degraded'
        : bus.health === 'unavailable'
          ? 'critical'
          : 'unknown'
  return { def, state, summary: bus.topReason, env }
}

export function ibGatewayApiVerdict(
  def: NonNullable<ReturnType<typeof getSignalDef>>,
  ib: IbGatewayStatusResponse | null | undefined,
): EvaluatedSignal {
  if (ib == null) {
    return { def, state: 'unknown', summary: 'IB Gateway status missing', env: 'shared' }
  }
  // Best-effort across known shapes — do not invent health.
  const reach =
    (ib as { reachability?: string }).reachability ??
    (ib as { status?: string }).status ??
    (ib as { overall?: string }).overall
  if (reach == null) {
    return { def, state: 'unknown', summary: 'IB Gateway status shape unrecognized', env: 'shared' }
  }
  const r = String(reach).toLowerCase()
  if (r === 'ok' || r === 'healthy' || r === 'connected' || r === 'ready') {
    return { def, state: 'healthy', summary: `IB Gateway ${reach}`, env: 'shared' }
  }
  if (r === 'degraded' || r === 'partial') {
    return { def, state: 'degraded', summary: `IB Gateway ${reach}`, env: 'shared' }
  }
  if (r === 'fail' || r === 'down' || r === 'unavailable' || r === 'disconnected') {
    return { def, state: 'critical', summary: `IB Gateway ${reach}`, env: 'shared' }
  }
  return { def, state: 'unknown', summary: `IB Gateway ${reach}`, env: 'shared' }
}

export function evaluateIbGateway(
  ib: IbGatewayStatusResponse | null | undefined,
  metrics: TelemetryMetricResult[] | null | undefined,
): EvaluatedSignal {
  const def = getSignalDef('subcontractors.ib-gateway')!
  const result = ibGatewayApiVerdict(def, ib)
  // Prometheus `ib_gateway_up` is evidence-only: the ib_gateway API probe above
  // stays the single verdict source — never a second judge.
  const up = metricById(metrics, 'ib_gateway_up')
  if (up != null && up.status === 'ok' && up.points.length > 0) {
    const promNote = `prometheus ib_gateway_up=${up.points.map(p => p.value).join(',')}`
    return {
      ...result,
      evidence: result.evidence != null ? `${result.evidence}; ${promNote}` : promNote,
    }
  }
  return result
}

export function goldenFromMetrics(metrics: TelemetryMetricResult[] | null | undefined): GoldenSignalRow[] {
  const ids = [
    'api_request_rate',
    'api_latency_p99',
    'api_error_rate',
    'redis_memory_bytes',
    'pg_connections',
  ]
  return ids.map(id => {
    const m = metricById(metrics, id)
    if (m == null) {
      return { id, label: id, status: 'unknown' as const, valueLabel: '—' }
    }
    if (m.status === 'error') {
      return { id, label: m.title, unit: m.unit, status: 'error', valueLabel: 'error', detail: m.detail }
    }
    if (m.status === 'empty' || m.points.length === 0) {
      return { id, label: m.title, unit: m.unit, status: 'empty', valueLabel: '—' }
    }
    const v = m.points[0]!.value
    let valueLabel = String(v)
    if (m.unit === 'ratio') valueLabel = `${(v * 100).toFixed(2)}%`
    else if (m.unit === 'bytes') valueLabel = `${(v / 1_048_576).toFixed(1)} MiB`
    else if (m.unit === 's') valueLabel = `${(v * 1000).toFixed(0)} ms`
    else if (m.unit === 'req/s') valueLabel = v.toFixed(3)
    return { id, label: m.title, unit: m.unit, status: 'ok', valueLabel }
  })
}

