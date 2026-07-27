import type { ReactNode } from 'react'
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag } from '@bifrost/ui'
import { fetchSatelliteBusDeep, fetchMatrix, isAllMatrices, isAllSatelliteBusDeep } from '@/api/core'
import {
  fetchClusterMetrics,
  fetchClusterObservability,
  fetchClusterServiceReadiness,
  fetchClusterWorkloads,
} from '@/api/cluster'
import type { MatrixResponse, Reachability, Target } from '@/api/matrixTypes'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import { StatusLamp } from '@/components/StatusLamp'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import {
  clearSatelliteBusTradeEnvFocus,
  peekSatelliteBusTradeEnvFocus,
} from '@/lib/activity/activityPageFocus'
import { useInFlightBusWorkload } from '@/components/activity/BusActuationStrip'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildSatelliteBusIngestTriagePrompt,
  SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
  summarizeIngestServices,
} from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { projectPayloadReadinessRows } from '@/lib/control-room/payloadReadiness'
import {
  buildSocketHealthMatrix,
  formatBusProbeDetail,
  summarizeSocketHealthAllEnvs,
  type BusEnvId,
} from '@/lib/satellite/socketHealthSemantics'
import { filterTradeApiTargets, tradeApiTargetCounts } from '@/lib/satellite/tradeApiTargets'
import {
  buildSatelliteBusViewModel,
  type BusHealth,
} from '@/lib/satellite-bus/satelliteBusViewModel'

/** Map 'unknown' → 'degraded' for UI rendering — no gray/? lamps on the bus page. */
function busLampReach(r: Reachability | undefined): Reachability {
  if (r == null || r === 'unknown') return 'degraded'
  return r
}

export type TradeEnvBusHealthSummary = {
  health: BusHealth
  healthLabel: string
  topReason: string
}

export const TRADE_ENV_OPTIONS = [
  { value: 'dev', label: 'Dev' },
  { value: 'stg', label: 'Stg' },
  { value: 'prod', label: 'Prod' },
] as const

export type TradeEnv = (typeof TRADE_ENV_OPTIONS)[number]['value']

export const TRADE_NS: Record<TradeEnv, string> = {
  dev: 'bifrost-dev',
  stg: 'bifrost-stg',
  prod: 'bifrost-prod',
}

export const SATELLITE_DOMAIN_IDS = ['workers', 'applications', 'database', 'redis'] as const

const CRITICAL_PROCESS_PATTERNS = [
  { pattern: /daemon/i, label: 'GsTrading daemon' },
  { pattern: /ingestor|ib-ingest/i, label: 'IB Ingestor' },
  { pattern: /operator|ib-operator/i, label: 'IB Operator' },
  { pattern: /account/i, label: 'IB Account Agent' },
  { pattern: /massive/i, label: 'Massive WS' },
  { pattern: /celery|worker/i, label: 'Celery worker' },
  { pattern: /flower/i, label: 'Flower' },
]

export type MonitorKvRow = { label: string; value: ReactNode }

export type CriticalProcessRow = {
  label: string
  name: string
  namespace: string
  reachability: Reachability
  ready: string
  status: string
}

function renderText(value: unknown): string {
  if (value == null) return '—'
  if (typeof value === 'string' && value.trim() === '') return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

/**
 * Satellite Bus data layer — matrix / bus-deep / cluster probes + derived view model.
 * Verdict derivation stays in buildSatelliteBusViewModel (do not re-derive here).
 */
export function useSatelliteBusQueries({
  ambientJobId,
  onStartAgentJob,
}: AmbientAgentShellProps = {}) {
  const { canOperate } = usePlatformAuth()
  const [tradeEnv, setTradeEnv] = useState<TradeEnv>(() => {
    const env = peekSatelliteBusTradeEnvFocus()
    if (env != null) {
      clearSatelliteBusTradeEnvFocus()
      return env
    }
    return 'stg'
  })
  const ns = TRADE_NS[tradeEnv]

  const matrixQuery = useQuery({
    queryKey: ['matrix', 'all'],
    queryFn: () => fetchMatrix(),
    refetchInterval: 30_000,
  })

  const serviceReadinessQuery = useQuery({
    queryKey: ['cluster', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 30_000,
  })

  const inFlightWorkload = useInFlightBusWorkload(ns)
  const workloadsQuery = useQuery({
    queryKey: ['cluster', 'workloads', ns, 'satellite-bus'],
    queryFn: () => fetchClusterWorkloads(ns),
    // Faster poll while Operate / ACTUATION strip is watching a rollout.
    refetchInterval: inFlightWorkload != null ? 2_000 : 30_000,
  })

  const metricsQuery = useQuery({
    queryKey: ['cluster', 'metrics'],
    queryFn: () => fetchClusterMetrics(8),
    refetchInterval: 30_000,
  })

  const observabilityQuery = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    retry: false,
  })

  const matrices = useMemo((): MatrixResponse[] => {
    const data = matrixQuery.data
    if (data == null) return []
    if (isAllMatrices(data)) return data.matrices
    return [data]
  }, [matrixQuery.data])

  const { fleet } = useFleetSnapshot()
  const payloadRows = useMemo(() => projectPayloadReadinessRows(fleet), [fleet])
  const envMatrix = matrices.find(m => m.environment === tradeEnv)
  const tradeApi = tradeApiTargetCounts(envMatrix)

  const busDeepAllQuery = useQuery({
    queryKey: ['satellite', 'bus-deep', 'all'],
    queryFn: () => fetchSatelliteBusDeep(),
    refetchInterval: 30_000,
  })

  const busesByEnv = useMemo((): Partial<Record<BusEnvId, SatelliteBusDeepResponse>> => {
    const data = busDeepAllQuery.data
    if (data == null) return {}
    if (isAllSatelliteBusDeep(data)) {
      return Object.fromEntries(
        data.buses.map(b => [b.environment as BusEnvId, b]),
      ) as Partial<Record<BusEnvId, SatelliteBusDeepResponse>>
    }
    const env = data.environment as BusEnvId
    if (env === 'dev' || env === 'stg' || env === 'prod' || env === 'dev-local') {
      return { [env]: data }
    }
    return {}
  }, [busDeepAllQuery.data])

  const busDeep = busesByEnv[tradeEnv]

  const viewModel = useMemo(
    () =>
      buildSatelliteBusViewModel({
        selectedEnv: tradeEnv,
        buses: busesByEnv,
        tradeApi,
      }),
    [busesByEnv, tradeApi, tradeEnv],
  )

  /** Per-env bus verdict for Trade NS selector lamps (no need to click each env). */
  const envHealthByEnv = useMemo((): Record<TradeEnv, TradeEnvBusHealthSummary> => {
    const out = {} as Record<TradeEnv, TradeEnvBusHealthSummary>
    for (const env of TRADE_ENV_OPTIONS) {
      const m = matrices.find(x => x.environment === env.value)
      const api = tradeApiTargetCounts(m)
      const vm = buildSatelliteBusViewModel({
        selectedEnv: env.value,
        buses: busesByEnv,
        tradeApi: api,
      })
      out[env.value] = {
        health: vm.health,
        healthLabel: vm.healthLabel,
        topReason: vm.topReason,
      }
    }
    return out
  }, [busesByEnv, matrices])

  const busLoading = busDeepAllQuery.isLoading && !busDeepAllQuery.isError
  const busProbeError = useMemo(() => {
    if (busDeepAllQuery.isError) {
      return busDeepAllQuery.error instanceof Error
        ? busDeepAllQuery.error.message
        : 'Satellite bus-deep probe failed'
    }
    if (matrixQuery.isError) {
      return matrixQuery.error instanceof Error
        ? matrixQuery.error.message
        : 'Connectivity matrix request failed'
    }
    return null
  }, [
    busDeepAllQuery.error,
    busDeepAllQuery.isError,
    matrixQuery.error,
    matrixQuery.isError,
  ])

  const criticalProcesses = useMemo((): CriticalProcessRow[] => {
    const workloads = workloadsQuery.data?.workloads ?? []
    return CRITICAL_PROCESS_PATTERNS.map(({ pattern, label }) => {
      const match = workloads.find(w => pattern.test(w.name))
      return {
        label,
        name: match?.name ?? '—',
        namespace: match?.namespace ?? ns,
        reachability: match?.reachability ?? ('unknown' as Reachability),
        ready: match?.ready ?? '—',
        status: match?.status ?? 'not deployed',
      }
    })
  }, [ns, workloadsQuery.data?.workloads])

  const ingestSummary = useMemo(
    () => summarizeIngestServices(busDeep?.ingest.services ?? []),
    [busDeep?.ingest.services],
  )

  const socketHealthMatrix = useMemo(() => {
    const probeDetailFor = (env: BusEnvId): string | undefined => {
      const bus = busesByEnv[env]
      if (bus == null) return undefined
      return formatBusProbeDetail(bus)
    }
    const slices = Object.fromEntries(
      (['dev', 'stg', 'prod', 'dev-local'] as const).map(env => [
        env,
        busesByEnv[env] != null
          ? {
              socket: busesByEnv[env]?.monitor.socket,
              ingest: busesByEnv[env]?.ingest.services,
              daemon: busesByEnv[env]?.monitor.daemon,
              probeDetail: probeDetailFor(env),
            }
          : undefined,
      ]),
    )
    return buildSocketHealthMatrix(slices)
  }, [busesByEnv])

  const socketSummary = useMemo(
    () => summarizeSocketHealthAllEnvs(socketHealthMatrix),
    [socketHealthMatrix],
  )

  const tradeApiTargetRows = useMemo(
    (): Target[] => (envMatrix != null ? filterTradeApiTargets(envMatrix) : []),
    [envMatrix],
  )

  const aiIngestTriage = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
    label: scopeToLabel(SATELLITE_BUS_INGEST_TRIAGE_SCOPE),
    buildRequest: () => ({
      prompt: buildSatelliteBusIngestTriagePrompt({
        env: tradeEnv,
        namespace: ns,
        ingestHeadline: ingestSummary.headline,
        socketHeadline: socketSummary.headline,
        busReachability: busDeep?.reachability,
      }),
    }),
  })

  const daemonRows = useMemo((): MonitorKvRow[] => {
    if (busLoading) return []
    const daemon = busDeep?.monitor.daemon
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={busLampReach(daemon?.reachability)} kind="reach" />{' '}
            {renderText(daemon?.reachability)}
          </>
        ),
      },
      { label: 'Self check', value: renderText(daemon?.self_check) },
      { label: 'Lamp', value: renderText(daemon?.lamp) },
      {
        label: 'Block reasons',
        value:
          (daemon?.block_reasons ?? []).length === 0
            ? '—'
            : (daemon?.block_reasons ?? []).map(reason => (
                <DenseTag key={reason} variant="warning" className="mr-1 text-[9px]">
                  {reason}
                </DenseTag>
              )),
      },
      { label: 'Trading suspended', value: renderText(daemon?.trading?.trading_suspended) },
      { label: 'daemon_alive', value: renderText(daemon?.heartbeat?.daemon_alive) },
      { label: 'ib_connected', value: renderText(daemon?.heartbeat?.ib_connected) },
      { label: 'seconds_until_retry', value: renderText(daemon?.heartbeat?.seconds_until_retry) },
    ]
  }, [busDeep?.monitor.daemon, busLoading])

  const celeryRows = useMemo((): MonitorKvRow[] => {
    const celery = busDeep?.monitor.celery
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={busLampReach(celery?.reachability)} kind="reach" />{' '}
            {renderText(celery?.reachability)}
          </>
        ),
      },
      { label: 'broker_connected', value: renderText(celery?.broker_connected) },
      { label: 'workers', value: (celery?.workers ?? []).join(', ') || '—' },
      { label: 'worker_ib_connected', value: renderText(celery?.worker_ib_connected) },
      { label: 'worker_ib_client_id', value: renderText(celery?.worker_ib_client_id) },
    ]
  }, [busDeep?.monitor.celery])

  const accountSyncRows = useMemo((): MonitorKvRow[] => {
    const sync = busDeep?.monitor.account_sync
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={busLampReach(sync?.reachability)} kind="reach" />{' '}
            {renderText(sync?.reachability)}
          </>
        ),
      },
      { label: 'daemon_alive', value: renderText(sync?.daemon_alive) },
      { label: 'stream_lag', value: renderText(sync?.stream_lag) },
    ]
  }, [busDeep?.monitor.account_sync])

  const opsRows = useMemo((): MonitorKvRow[] => {
    const ops = busDeep?.ops
    return [
      {
        label: 'Reachability',
        value: (
          <>
            <StatusLamp value={busLampReach(ops?.reachability)} kind="reach" /> {renderText(ops?.reachability)}
          </>
        ),
      },
      { label: 'executor_mode', value: renderText(ops?.executor_mode) },
      { label: 'k8s_reachable', value: renderText(ops?.k8s_reachable) },
      { label: 'status', value: renderText(ops?.status) },
    ]
  }, [busDeep?.ops])

  const probeTime =
    busDeep?.generated_at != null
      ? new Date(busDeep.generated_at).toLocaleTimeString()
      : envMatrix?.generated_at != null
        ? new Date(envMatrix.generated_at).toLocaleTimeString()
        : '—'

  return {
    tradeEnv,
    setTradeEnv,
    ns,
    viewModel,
    envHealthByEnv,
    busDeep,
    busLoading,
    busProbeError,
    matrixQuery,
    serviceReadinessQuery,
    workloadsQuery,
    metricsQuery,
    observabilityQuery,
    payloadRows,
    socketHealthMatrix,
    tradeApiTargetRows,
    criticalProcesses,
    aiIngestTriage,
    canOperate,
    daemonRows,
    celeryRows,
    accountSyncRows,
    opsRows,
    probeTime,
  }
}
