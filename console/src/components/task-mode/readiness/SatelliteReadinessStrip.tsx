import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn } from '@bifrost/ui'
import { Satellite } from 'lucide-react'
import { fetchCluster } from '@/api/cluster'
import { fetchReleaseGate } from '@/api/promote'
import { fetchSatelliteBusDeep, isAllSatelliteBusDeep } from '@/api/core'
import type { SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { infraSignal, missionStatus, worst } from '@/lib/control-room/missionSignals'
import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { usePromoteVerifyReadiness, useSatelliteProdReadiness } from './hooks'
import {
  datastoreDetail,
  datastoreEnvSignal,
  isProdReleaseBlocked,
  namespacePods,
  PROD_NS,
  REFETCH_MS,
  releaseGateSignal,
  sharedRocketFromSocket,
  STG_NS,
  tradeApiSummary,
  type EnvChip,
} from './utils'
import { EnvironmentReadinessPanel, ProdBlockedBanner, SharedRocketStrip } from './RocketReadinessStrip'

/** Self-contained shared IB bus panel for Mission Launch coupling strip. */
export function MissionSharedBusPanel({
  compact = false,
  onNavigate,
  canOperate = false,
}: {
  compact?: boolean
  onNavigate: (tabId: string) => void
  canOperate?: boolean
}) {
  const { rocketSignal, rocketDetail } = useSatelliteProdReadiness()

  const stgBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: REFETCH_MS,
  })

  const prodBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
  })

  const busForEnv = (data: typeof stgBusQ.data, env: 'stg' | 'prod'): SatelliteBusDeepResponse | undefined => {
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === env)
    return data
  }

  const stgBusReach = useMemo(() => busForEnv(stgBusQ.data, 'stg'), [stgBusQ.data])
  const prodBusReach = useMemo(() => busForEnv(prodBusQ.data, 'prod'), [prodBusQ.data])

  const rocket = useMemo(() => {
    if (prodBusReach != null) return sharedRocketFromSocket(prodBusReach.monitor.socket)
    if (stgBusReach != null) return sharedRocketFromSocket(stgBusReach.monitor.socket)
    return { signal: rocketSignal, detail: rocketDetail }
  }, [prodBusReach, stgBusReach, rocketSignal, rocketDetail])

  return (
    <SharedRocketStrip
      rocket={rocket}
      isLoading={stgBusQ.isLoading || prodBusQ.isLoading}
      compact={compact}
      onNavigate={onNavigate}
      canOperate={canOperate}
    />
  )
}

export function SatelliteReadinessStrip({
  compact = false,
  summaryColumn = false,
  suppressProdBlockedBanner = false,
  omitSharedBus = false,
  onNavigate,
  canOperate = false,
  onAgentFixStg,
  onAgentFixProd,
  agentFixPending = false,
  agentFixDisabled = false,
  agentFixTitle,
}: {
  compact?: boolean
  summaryColumn?: boolean
  suppressProdBlockedBanner?: boolean
  /** When true, IB bus is rendered by Mission Launch shared strip instead. */
  omitSharedBus?: boolean
  onNavigate: (tabId: string) => void
  canOperate?: boolean
  onAgentFixStg?: () => void
  onAgentFixProd?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
}) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()
  const {
    tradeProdOverall,
    tradeProdBlocked,
    rocketBlocked,
    rocketSignal,
    rocketDetail,
  } = useSatelliteProdReadiness()

  const stgBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: REFETCH_MS,
  })

  const prodBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: REFETCH_MS,
  })

  const stgMatrix = useMemo(() => matrices.find(m => m.environment === 'stg'), [matrices])
  const prodMatrix = useMemo(() => matrices.find(m => m.environment === 'prod'), [matrices])

  const clusterDetailQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
  })

  const cluster = clusterDetailQ.data

  const stgK8s = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, STG_NS)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const prodK8s = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, PROD_NS)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const stgDatastore = useMemo(
    () => ({
      signal: datastoreEnvSignal(matrices, 'stg'),
      detail: datastoreDetail(matrices, 'stg'),
    }),
    [matrices],
  )

  const prodDatastore = useMemo(
    () => ({
      signal: datastoreEnvSignal(matrices, 'prod'),
      detail: datastoreDetail(matrices, 'prod'),
    }),
    [matrices],
  )

  const busForEnv = (data: typeof stgBusQ.data, env: 'stg' | 'prod'): SatelliteBusDeepResponse | undefined => {
    if (data == null) return undefined
    if (isAllSatelliteBusDeep(data)) return data.buses.find(b => b.environment === env)
    return data
  }

  const stgBusReach = useMemo(() => busForEnv(stgBusQ.data, 'stg'), [stgBusQ.data])
  const prodBusReach = useMemo(() => busForEnv(prodBusQ.data, 'prod'), [prodBusQ.data])

  const rocket = useMemo(() => {
    if (prodBusReach != null) return sharedRocketFromSocket(prodBusReach.monitor.socket)
    if (stgBusReach != null) return sharedRocketFromSocket(stgBusReach.monitor.socket)
    return { signal: rocketSignal, detail: rocketDetail }
  }, [prodBusReach, stgBusReach, rocketSignal, rocketDetail])

  const stgTradeApis = useMemo(() => tradeApiSummary(stgMatrix), [stgMatrix])
  const prodTradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])
  const promoteVerify = usePromoteVerifyReadiness()

  const stgOverall = worst(
    stgK8s.signal,
    stgDatastore.signal,
    stgTradeApis.signal,
    promoteVerify.stgReleaseSignal,
  )
  const prodOverallLocal = worst(
    prodK8s.signal,
    prodDatastore.signal,
    prodTradeApis.signal,
    snapshot.tradeProd.signal,
    prodGate.signal,
    promoteVerify.promoteSignal,
  )

  const stgLoading = missionLoading || clusterDetailQ.isLoading || promoteVerify.isLoading
  const prodLoading =
    missionLoading || clusterDetailQ.isLoading || prodGateQ.isLoading || promoteVerify.isLoading
  const rocketLoading = stgBusQ.isLoading || prodBusQ.isLoading

  const showProdBanner =
    !suppressProdBlockedBanner &&
    (tradeProdBlocked || isProdReleaseBlocked(prodOverallLocal) || isProdReleaseBlocked(tradeProdOverall))

  const prodChips: EnvChip[] = [
    { label: 'Trade · K8s PROD', signal: prodK8s.signal, detail: prodK8s.detail, fixScope: PROD_ENV_FIX_SCOPE },
    {
      label: 'Ground · PG / Redis',
      signal: prodDatastore.signal,
      detail: prodDatastore.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
    {
      label: 'Trade · APIs PROD',
      signal: prodTradeApis.signal,
      detail: prodTradeApis.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
    {
      label: 'Trade · PROD matrix',
      signal: snapshot.tradeProd.signal,
      detail: snapshot.tradeProd.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
    {
      label: 'Trade · PROD gate',
      signal: prodGate.signal,
      detail: prodGate.detail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
    {
      label: 'Promote / cutover',
      signal: promoteVerify.promoteSignal,
      detail: promoteVerify.promoteDetail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
  ]

  return (
    <div className={cn('flex min-h-0 flex-col', summaryColumn ? 'h-full gap-1.5' : 'gap-2')}>
      {showProdBanner && <ProdBlockedBanner context="satellite" />}
      {!omitSharedBus && (
        <SharedRocketStrip
          rocket={rocket}
          isLoading={rocketLoading}
          compact={compact || summaryColumn}
          onNavigate={onNavigate}
          canOperate={canOperate}
        />
      )}
      <div
        className={cn(
          'grid min-h-0 gap-2',
          summaryColumn ? 'flex-1 md:grid-cols-2 [&>*]:h-full' : 'gap-2',
        )}
      >
        <EnvironmentReadinessPanel
          title={summaryColumn ? 'STG trade' : 'STG environment readiness'}
          icon={Satellite}
          overall={stgOverall}
          isLoading={stgLoading}
          compact={compact}
          summaryColumn={summaryColumn}
          readinessAnchor="stg"
          chips={[
            { label: 'Trade · K8s STG', signal: stgK8s.signal, detail: stgK8s.detail, fixScope: PROD_ENV_FIX_SCOPE },
            {
              label: 'Ground · PG / Redis',
              signal: stgDatastore.signal,
              detail: stgDatastore.detail,
              fixScope: PROD_ENV_FIX_SCOPE,
            },
            {
              label: 'Trade · APIs STG',
              signal: stgTradeApis.signal,
              detail: stgTradeApis.detail,
              fixScope: PROD_ENV_FIX_SCOPE,
            },
            {
              label: 'STG release',
              signal: promoteVerify.stgReleaseSignal,
              detail: promoteVerify.stgReleaseDetail,
              fixScope: DELIVER_STG_RECOVER_SCOPE,
            },
          ]}
          linkLabel="Satellite Bus →"
          onLink={() => onNavigate('satellite-bus')}
          onNavigate={onNavigate}
          fixCtx={{ modeId: 'mission-launch', env: 'stg' }}
          canOperate={canOperate}
          onAgentFix={onAgentFixStg}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
        <EnvironmentReadinessPanel
          title={summaryColumn ? 'Trade Prod' : 'PROD environment readiness'}
          icon={Satellite}
          overall={prodOverallLocal}
          isLoading={prodLoading}
          compact={compact}
          summaryColumn={summaryColumn}
          readinessAnchor="trade-prod"
          chips={prodChips}
          linkLabel="Deploy Satellite →"
          onLink={() => onNavigate('trade-release')}
          onNavigate={onNavigate}
          fixCtx={{ modeId: 'mission-launch', env: 'prod' }}
          canOperate={canOperate}
          onAgentFix={onAgentFixProd}
          agentFixPending={agentFixPending}
          agentFixDisabled={agentFixDisabled}
          agentFixTitle={agentFixTitle}
        />
      </div>
      {!suppressProdBlockedBanner && rocketBlocked && !showProdBanner && (
        <OpsFeedback variant="warning" title="Fix Rocket IB bus before release">
          Shared Rocket readiness is {missionStatus(rocket.signal)} — resolve Platform IB Gateway /
          socket consumers before deploying.
        </OpsFeedback>
      )}
    </div>
  )
}
