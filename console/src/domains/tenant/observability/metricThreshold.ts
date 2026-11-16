import type { TelemetryMetricResult } from '@/api/clusterTypes'
import type { EvaluatedSignal, ObservabilityEnvId, SignalDef } from '@/domains/foundation/observability/types'
export function tenantMetricThreshold(signalId: string, metrics: TelemetryMetricResult[] | null | undefined, def: SignalDef, env: ObservabilityEnvId): EvaluatedSignal | undefined {
  // Error-rate threshold: any service > 5% → degraded; > 20% → critical
  if (signalId === 'satellite.api-error-rate') {
    const err = metrics?.find(m => m.id === 'api_error_rate')
    const max = Math.max(0, ...(err?.points.map(p => p.value) ?? [0]))
    if (max > 0.2) {
      return { def, state: 'critical', summary: `5xx rate ${(max * 100).toFixed(1)}%`, env }
    }
    if (max > 0.05) {
      return { def, state: 'degraded', summary: `5xx rate ${(max * 100).toFixed(1)}%`, env }
    }
  }

  return undefined
}
