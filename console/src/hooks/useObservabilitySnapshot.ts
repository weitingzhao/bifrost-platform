/**
 * Observability hub data hook — aggregates existing telemetry / cluster / bus probes.
 * Verdict derivation lives solely in buildObservabilityViewModel (do not re-derive in pages).
 */

import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchAgentBridge } from '@/api/agentOps'
import { fetchClusterMetrics, fetchClusterNodes, fetchClusterObservability } from '@/api/cluster'
import { fetchIbGatewayStatus } from '@/api/network'
import { fetchMatrix, fetchSatelliteBusDeep, fetchSelfHealth, isAllMatrices, isAllSatelliteBusDeep } from '@/api/core'
import { fetchRemediationHealth } from '@/api/remediation'
import { fetchTelemetryAlerts, fetchTelemetryOverview, fetchTelemetryTargets } from '@/api/telemetry'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { SystemDomainId } from '@/lib/architecture/systemDomainCatalog'
import { normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot'
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

/** Map Fleet viewer seat → Trade NS (dev-local / unknown → dev). */
export function tradeEnvFromViewer(raw: string | undefined | null): TradeEnv {
  const v = normalizeViewerEnv(raw)
  if (v === 'prod') return 'prod'
  if (v === 'stg') return 'stg'
  return 'dev'
}

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
  // Seed from VITE_OPS_VIEWER_ENV; hydrate from selfHealth.viewer_env once (unless user overrides).
  const [tradeEnv, setTradeEnvState] = useState<TradeEnv>(() =>
    tradeEnvFromViewer(import.meta.env.VITE_OPS_VIEWER_ENV),
  )
  const tradeEnvTouchedRef = useRef(false)
  const setTradeEnv = (env: TradeEnv) => {
    tradeEnvTouchedRef.current = true
    setTradeEnvState(env)
  }

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
  // Elastic standby hosts — suppress Expected-Off node alert / scrape noise.
  const nodesQ = useQuery({
    queryKey: ['cluster', 'nodes'],
    queryFn: fetchClusterNodes,
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
  // Scope to selected Trade NS — all-env bus-deep storms Traefik NodePorts and
  // falsely marks IB consumers down (context deadline exceeded).
  const busQ = useQuery({
    queryKey: ['satellite', 'bus-deep', tradeEnv],
    queryFn: () => fetchSatelliteBusDeep(tradeEnv),
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

  // First hydrate from self-health viewer_env; never overwrite after manual Trade NS change.
  useEffect(() => {
    if (tradeEnvTouchedRef.current) return
    const viewer = selfQ.data?.viewer_env
    if (viewer == null || viewer === '') return
    setTradeEnvState(tradeEnvFromViewer(viewer))
  }, [selfQ.data?.viewer_env])

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
    // Traefik NodePort flaps → bus-deep timeout looks like "IB consumer down".
    // Prefer Platform IB Gateway plugin when the failure is clearly a probe timeout.
    const probeTimeout = /context deadline|Timeout exceeded|Client\.Timeout/i.test(vm.topReason)
    const pluginOk =
      ibQ.data?.reachability === 'ok' ||
      ibQ.data?.reachable === true ||
      String(ibQ.data?.summary ?? '').toLowerCase().includes('redis-ib ok')
    if (probeTimeout && pluginOk && (vm.health === 'unavailable' || vm.health === 'unknown')) {
      return {
        health: 'healthy',
        topReason: `Monitor probe timed out; Platform IB Gateway plugin ok — ${ibQ.data?.summary ?? 'reachable'}`,
      }
    }
    return { health: vm.health, topReason: vm.topReason }
  }, [busQ.data, matrixQ.data, tradeEnv, ibQ.data])

  const standbyNodes = useMemo(
    () =>
      (nodesQ.data?.nodes ?? [])
        .filter(n => n.elastic_mode === 'standby')
        .map(n => ({ name: n.name, internalIp: n.internal_ip || undefined })),
    [nodesQ.data?.nodes],
  )

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
        standbyNodes,
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
      standbyNodes,
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
    void nodesQ.refetch()
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
