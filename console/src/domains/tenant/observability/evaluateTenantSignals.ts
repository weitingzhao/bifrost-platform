import type { ObservabilityViewModelInput } from '@/domains/foundation/observability/observabilityViewModel'
import type { EvaluatedSignal, ObservabilityEnvId } from '@/domains/foundation/observability/types'
import { getSignalDef } from '@/domains/foundation/observability/signalRegistry'
import { evaluateBus, evaluateIbGateway } from './probeInterpretation'
export function evaluateTenantSignals(input: ObservabilityViewModelInput, evaluateMetricSignal: (signalId: string, metrics: ObservabilityViewModelInput['telemetryMetrics'], telemetryError: ObservabilityViewModelInput['telemetryError'], env: ObservabilityEnvId) => EvaluatedSignal | null): EvaluatedSignal[] {
  const env = input.selectedEnv
  const signals: EvaluatedSignal[] = []
  // Satellite
  for (const id of [
    'satellite.api-request-rate',
    'satellite.api-latency-p99',
    'satellite.api-error-rate',
  ] as const) {
    const s = evaluateMetricSignal(id, input.telemetryMetrics, input.telemetryError, env)
    if (s != null) signals.push(s)
  }
  signals.push(evaluateBus(input.bus, env))
  // K8s workloads — evidence only, always unknown unless explicitly supplied later
  {
    const def = getSignalDef('satellite.k8s-workloads')!
    signals.push({
      def,
      state: 'unknown',
      summary: 'Workload readiness is evidence-only (see Satellite Bus Evidence)',
      env,
    })
  }

  // Subcontractors
  signals.push(evaluateIbGateway(input.ibGateway, input.telemetryMetrics))

  return signals
}
