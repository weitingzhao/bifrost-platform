/**
 * Observability hub data hook — aggregates existing telemetry / cluster / bus probes.
 * Verdict derivation lives solely in buildObservabilityViewModel (do not re-derive in pages).
 */

import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  fetchAgentBridge,
  fetchClusterMetrics,
  fetchClusterObservability,
  fetchIbGatewayStatus,
  fetchMatrix,
  fetchRemediationHealth,
  fetchSatelliteBusDeep,
  fetchSelfHealth,
  fetchTelemetryAlerts,
  fetchTelemetryOverview,
  fetchTelemetryTargets,
  isAllMatrices,
  isAllSatelliteBusDeep,
} from '@/api/platform'
import type { MatrixResponse } from '@/api/types'
import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import {
  buildObservabilityViewModel,
  TRADE_NS,
  type BusHealthInput,
  type ObservabilityViewModel,
} from '@/lib/observability'
import { buildSatelliteBusViewModel } from '@/lib/satellite-bus/satelliteBusViewModel'
import type { BusEnvId } from '@/lib/satellite/socketHealthSemantics'
import { tradeApiTargetCounts } from '@/lib/satellite/tradeApiTargets'

const REFETCH = 30_000

export type TradeEnv = 'dev' | 'stg' | 'prod'

function errMessage(err: unknown): string | null {
  if (err == null) return null
  if (err instanceof Error) return err.message
  return String(err)
}

export function useObservabilitySnapshot(): {
  viewModel: ObservabilityViewModel
  tradeEnv: TradeEnv
  setTradeEnv: (env: TradeEnv) => void
  selectedDomain: SystemDomainId
  setSelectedDomain: (domain: SystemDomainId) => void
  isLoading: boolean
  isFetching: boolean
  refetchAll: () => void
  namespace: string
} {
  const [tradeEnv, setTradeEnv] = useState<TradeEnv>('stg')
  const [selectedDomain, setSelectedDomain] = useState<SystemDomainId>('satellite')
  const ns = TRADE_NS[tradeEnv]

  const observabilityQ = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: REFETCH,
    retry: false,
  })
  const metricsQ = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: REFETCH,
    retry: false,
  })
  const telemetryQ = useQuery({
    queryKey: ['telemetry', 'overview', ns],
    queryFn: () => fetchTelemetryOverview(ns),
    refetchInterval: REFETCH,
    retry: false,
  })
  const alertsQ = useQuery({
    queryKey: ['telemetry', 'alerts'],
    queryFn: fetchTelemetryAlerts,
    refetchInterval: REFETCH,
    retry: false,
  })
  const targetsQ = useQuery({
    queryKey: ['telemetry', 'targets'],
    queryFn: () => fetchTelemetryTargets(),
    refetchInterval: REFETCH,
    retry: false,
  })
  const busQ = useQuery({
    queryKey: ['satellite', 'bus-deep', 'all'],
    queryFn: () => fetchSatelliteBusDeep(),
    refetchInterval: REFETCH,
    retry: false,
  })
  // Same source as SatelliteBusPage — keeps the bus verdict inputs identical (SSOT).
  const matrixQ = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: REFETCH,
    retry: false,
  })
  const ibQ = useQuery({
    queryKey: ['ib-gateway', 'status'],
    queryFn: fetchIbGatewayStatus,
    refetchInterval: REFETCH,
    retry: false,
  })
  const remediationQ = useQuery({
    queryKey: ['remediation', 'health'],
    queryFn: fetchRemediationHealth,
    refetchInterval: REFETCH,
    retry: false,
  })
  const bridgeQ = useQuery({
    queryKey: ['agent', 'bridge'],
    queryFn: fetchAgentBridge,
    refetchInterval: REFETCH,
    retry: false,
  })
  const selfQ = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH,
    retry: false,
  })

  const busHealth: BusHealthInput | null = useMemo(() => {
    const data = busQ.data
    if (data == null) return null
    const buses = isAllSatelliteBusDeep(data)
      ? Object.fromEntries(data.buses.map(b => [b.environment as BusEnvId, b]))
      : { [data.environment as BusEnvId]: data }
    const matrixData = matrixQ.data
    let matrices: MatrixResponse[] = []
    if (matrixData != null) {
      matrices = isAllMatrices(matrixData) ? matrixData.matrices : [matrixData]
    }
    const envMatrix = matrices.find(m => m.environment === tradeEnv)
    const vm = buildSatelliteBusViewModel({
      selectedEnv: tradeEnv,
      buses,
      tradeApi: tradeApiTargetCounts(envMatrix),
    })
    return { health: vm.health, topReason: vm.topReason }
  }, [busQ.data, matrixQ.data, tradeEnv])

  const viewModel = useMemo(
    () =>
      buildObservabilityViewModel({
        selectedEnv: tradeEnv,
        selectedDomain,
        observability: observabilityQ.data,
        metrics: metricsQ.data,
        telemetryMetrics: telemetryQ.data?.metrics,
        telemetryError: errMessage(telemetryQ.error),
        alerts: alertsQ.data?.alerts,
        alertsError: errMessage(alertsQ.error),
        targets: targetsQ.data?.active_targets,
        targetsError: errMessage(targetsQ.error),
        bus: busHealth,
        ibGateway: ibQ.data,
        remediation: remediationQ.data,
        agentBridge: bridgeQ.data,
        selfHealth: selfQ.data,
      }),
    [
      tradeEnv,
      selectedDomain,
      observabilityQ.data,
      metricsQ.data,
      telemetryQ.data?.metrics,
      telemetryQ.error,
      alertsQ.data?.alerts,
      alertsQ.error,
      targetsQ.data?.active_targets,
      targetsQ.error,
      busHealth,
      ibQ.data,
      remediationQ.data,
      bridgeQ.data,
      selfQ.data,
    ],
  )

  const isLoading =
    observabilityQ.isLoading ||
    telemetryQ.isLoading ||
    alertsQ.isLoading ||
    targetsQ.isLoading

  const isFetching =
    observabilityQ.isFetching ||
    metricsQ.isFetching ||
    telemetryQ.isFetching ||
    alertsQ.isFetching ||
    targetsQ.isFetching ||
    busQ.isFetching ||
    matrixQ.isFetching ||
    ibQ.isFetching ||
    remediationQ.isFetching ||
    bridgeQ.isFetching ||
    selfQ.isFetching

  const refetchAll = () => {
    void observabilityQ.refetch()
    void metricsQ.refetch()
    void telemetryQ.refetch()
    void alertsQ.refetch()
    void targetsQ.refetch()
    void busQ.refetch()
    void matrixQ.refetch()
    void ibQ.refetch()
    void remediationQ.refetch()
    void bridgeQ.refetch()
    void selfQ.refetch()
  }

  return {
    viewModel,
    tradeEnv,
    setTradeEnv,
    selectedDomain,
    setSelectedDomain,
    isLoading,
    isFetching,
    refetchAll,
    namespace: ns,
  }
}
