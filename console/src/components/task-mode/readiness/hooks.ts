import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchContext, fetchSatelliteBusDeep, fetchSelfHealth } from '@/api/core'
import { fetchReleaseGate, fetchStgSmoke, fetchTierBStatus } from '@/api/promote'
import { fetchSupplyChain } from '@/api/delivery'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import {
  infraSignal,
  worst,
  type Signal,
} from '@/lib/control-room/missionSignals'
import type { ProdFixSignal } from '@/lib/agent/prodEnvironmentFixPrompt'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  DELIVER_STG_RECOVER_SCOPE,
  PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
} from '@/lib/agent/agentScopes'
import { SATELLITE_BUS_INGEST_TRIAGE_SCOPE } from '@/lib/agent/satelliteBusIngestTriagePrompt'
import {
  evaluatePromoteStatus,
  evaluateStgReleaseStatus,
} from '@/lib/control-room/matrixSummary'
import {
  promoteProdDetail,
  promoteVerifySignal,
  stgReleaseVerifySignal,
} from '@/lib/control-room/promoteCutover'
import { isPipelineRunSucceeded } from '@/lib/delivery/pipelineRunAskPack'
import {
  datastoreDetail,
  datastoreEnvSignal,
  isProdReleaseBlocked,
  namespacePods,
  accountSyncChipFromBus,
  busForEnv,
  PLATFORM_PROD,
  PLATFORM_STG,
  PROD_NS,
  REFETCH_MS,
  releaseGateSignal,
  selfHealthEnvSignal,
  sharedRocketFromSocket,
  STG_NS,
  tradeApiSummary,
} from './utils'

export type SatelliteBlockKind = 'rocket' | 'prod' | 'both' | null

/** Promote / cutover + STG release verify — shared Mission Launch readiness GO/NO-GO. */
export function usePromoteVerifyReadiness(enabled = true) {
  const { matrices } = useMissionSnapshot()

  const contextQ = useQuery({
    queryKey: ['context'],
    queryFn: fetchContext,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const stgSmokeQ = useQuery({
    queryKey: ['cockpit', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const stgGateQ = useQuery({
    queryKey: ['promote', 'release-gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const tierBQ = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const supplyQ = useQuery({
    queryKey: ['cockpit', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const lastDeliverSucceeded =
    supplyQ.data?.last_deliver_success != null &&
    isPipelineRunSucceeded(supplyQ.data.last_deliver_success)

  const promote = useMemo(() => {
    if (contextQ.data == null) return null
    return evaluatePromoteStatus(contextQ.data, matrices)
  }, [contextQ.data, matrices])

  const stgRelease = useMemo(() => {
    return evaluateStgReleaseStatus(
      stgSmokeQ.data,
      lastDeliverSucceeded,
      stgGateQ.data,
      tierBQ.data,
    )
  }, [stgSmokeQ.data, lastDeliverSucceeded, stgGateQ.data, tierBQ.data])

  const promoteSignal: Signal = promote != null ? promoteVerifySignal(promote) : 'unknown'
  const promoteDetail =
    promote != null
      ? promote.ready
        ? 'Spine promote · prod cutover verify clear'
        : promoteProdDetail(promote)
      : 'Loading promote verify…'

  const stgReleaseSignal: Signal = stgReleaseVerifySignal(stgRelease)
  const stgReleaseDetail = stgRelease.releaseReady
    ? 'Deliver + smoke + STG gate + Tier B complete'
    : stgRelease.releaseReasons[0] ?? 'STG release verify incomplete'

  return {
    promoteSignal,
    promoteDetail,
    promoteReady: promote?.ready === true,
    stgReleaseSignal,
    stgReleaseDetail,
    stgReleaseReady: stgRelease.releaseReady,
    isLoading:
      contextQ.isLoading ||
      stgSmokeQ.isLoading ||
      stgGateQ.isLoading ||
      tierBQ.isLoading ||
      supplyQ.isLoading,
  }
}

export function useSatelliteProdReadiness(enabled = true) {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const busQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodMatrix = useMemo(() => matrices.find(m => m.environment === 'prod'), [matrices])
  const cluster = clusterQ.data

  const k8s = useMemo(() => {
    const infra = infraSignal(cluster)
    const ns = namespacePods(cluster, PROD_NS)
    return { signal: worst(infra.signal, ns.signal), detail: `${infra.detail} · ${ns.detail}` }
  }, [cluster])

  const datastore = useMemo(
    () => ({
      signal: datastoreEnvSignal(matrices, 'prod'),
      detail: datastoreDetail(matrices, 'prod'),
    }),
    [matrices],
  )

  const busReach = useMemo(() => busForEnv(busQ.data, 'prod'), [busQ.data])

  const rocket = useMemo(
    () => sharedRocketFromSocket(busReach?.monitor.socket),
    [busReach],
  )

  const accountSync = useMemo(() => accountSyncChipFromBus(busReach, 'prod'), [busReach])

  const tradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const tradeSnapshot = snapshot.tradeProd
  const gate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  /** Trade Prod only — shared Rocket is reported separately. */
  const tradeProdOverall = worst(
    k8s.signal,
    datastore.signal,
    tradeApis.signal,
    accountSync.signal,
    tradeSnapshot.signal,
    gate.signal,
  )

  const releaseOverall = worst(tradeProdOverall, rocket.signal)

  const isLoading =
    missionLoading || clusterQ.isLoading || busQ.isLoading || prodGateQ.isLoading

  const tradeProdBlocked = isProdReleaseBlocked(tradeProdOverall)
  const rocketBlocked = isProdReleaseBlocked(rocket.signal)
  const prodBlocked = isProdReleaseBlocked(releaseOverall)

  const blockKind: SatelliteBlockKind = prodBlocked
    ? rocketBlocked && tradeProdBlocked
      ? 'both'
      : rocketBlocked
        ? 'rocket'
        : 'prod'
    : null

  const prodDisabledReason = !prodBlocked
    ? undefined
    : blockKind === 'rocket'
      ? 'Rocket IB bus blocked — fix shared gateway before deploy'
      : blockKind === 'both'
        ? 'Rocket IB bus and Trade Prod readiness blocked'
        : 'Prod readiness blocked — fix environment first'

  return {
    prodOverall: releaseOverall,
    tradeProdOverall,
    rocketSignal: rocket.signal,
    rocketDetail: rocket.detail,
    tradeProdBlocked,
    rocketBlocked,
    blockKind,
    prodBlocked,
    isLoading,
    prodDisabledReason,
    fixSignals: [
      { label: 'Trade · K8s PROD', signal: k8s.signal, detail: k8s.detail, fixScope: PROD_ENV_FIX_SCOPE },
      accountSync,
      {
        label: 'Ground · PG / Redis',
        signal: datastore.signal,
        detail: datastore.detail,
        fixScope: PROD_ENV_FIX_SCOPE,
      },
      {
        label: 'Trade · APIs PROD',
        signal: tradeApis.signal,
        detail: tradeApis.detail,
        fixScope: PROD_ENV_FIX_SCOPE,
      },
      {
        label: 'Trade · PROD matrix',
        signal: tradeSnapshot.signal,
        detail: tradeSnapshot.detail,
        fixScope: PROD_ENV_FIX_SCOPE,
      },
      {
        label: 'Trade · PROD gate',
        signal: gate.signal,
        detail: gate.detail,
        fixScope: DELIVER_STG_RECOVER_SCOPE,
      },
    ] as ProdFixSignal[],
    rocketFixSignal: {
      label: 'Rocket · IB socket',
      signal: rocket.signal,
      detail: rocket.detail,
      fixScope: SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
    } as ProdFixSignal,
    prodNamespace: PROD_NS,
    stgNamespace: STG_NS,
  }
}

export function useRocketProdReadiness(enabled = true) {
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const selfQ = useQuery({
    queryKey: ['cockpit', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const cluster = clusterQ.data
  const k8sProd = useMemo(() => namespacePods(cluster, PLATFORM_PROD), [cluster])
  const selfProd = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'prod'), [selfQ.data?.probes])
  const gate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const prodOverall = worst(k8sProd.signal, selfProd.signal, gate.signal, snapshot.release.signal)

  const isLoading = missionLoading || clusterQ.isLoading || selfQ.isLoading || prodGateQ.isLoading
  const prodBlocked = isProdReleaseBlocked(prodOverall)

  return {
    prodOverall,
    prodBlocked,
    isLoading,
    prodDisabledReason: prodBlocked ? 'Prod readiness blocked — fix environment first' : undefined,
    fixSignals: [
      {
        label: 'K8s · Platform PROD NS',
        signal: k8sProd.signal,
        detail: k8sProd.detail,
        fixScope: PROD_ENV_FIX_SCOPE,
      },
      {
        label: 'Self-health PROD',
        signal: selfProd.signal,
        detail: selfProd.detail,
        fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      },
      {
        label: 'PROD release gate',
        signal: gate.signal,
        detail: gate.detail,
        fixScope: DELIVER_STG_RECOVER_SCOPE,
      },
      {
        label: 'Supply chain',
        signal: snapshot.release.signal,
        detail: snapshot.release.detail,
        fixScope: DELIVER_STG_RECOVER_SCOPE,
      },
    ] as ProdFixSignal[],
    prodNamespace: PLATFORM_PROD,
  }
}

export type LaunchViewOverall = {
  overall: Signal
  stgOverall: Signal
  prodOverall: Signal
  rocketSignal?: Signal
  rocketDetail?: string
  isLoading: boolean
  fixSignals: ProdFixSignal[]
}

/** Headline readiness for Rocket Launch view — worst(STG, PROD) panels in Task CC. */
export function useRocketLaunchOverall(enabled = true): LaunchViewOverall {
  const { snapshot, isLoading: missionLoading } = useMissionSnapshot()

  const serviceQ = useQuery({
    queryKey: ['task-cc', 'service-readiness'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const selfQ = useQuery({
    queryKey: ['cockpit', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const stgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const cicdDomain = serviceQ.data?.domains.find(d => d.id === 'cicd')
  const cicdSignal = (cicdDomain?.reachability ?? 'unknown') as Signal
  const cicdDetail = cicdDomain?.summary ?? serviceQ.data?.detail ?? 'Tekton · platform namespaces'

  const cluster = clusterQ.data
  const clusterInfra = useMemo(() => infraSignal(cluster), [cluster])
  const k8sStgNs = useMemo(() => namespacePods(cluster, PLATFORM_STG), [cluster])
  const k8sProd = useMemo(() => namespacePods(cluster, PLATFORM_PROD), [cluster])
  const selfStg = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'stg'), [selfQ.data?.probes])
  const selfProd = useMemo(() => selfHealthEnvSignal(selfQ.data?.probes, 'prod'), [selfQ.data?.probes])
  const stgGate = useMemo(() => releaseGateSignal(stgGateQ.data), [stgGateQ.data])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(
    clusterInfra.signal,
    k8sStgNs.signal,
    cicdSignal,
    selfStg.signal,
    stgGate.signal,
    snapshot.release.signal,
  )
  const prodOverall = worst(k8sProd.signal, selfProd.signal, prodGate.signal, snapshot.release.signal)
  const overall = worst(stgOverall, prodOverall)

  const isLoading =
    missionLoading ||
    serviceQ.isLoading ||
    clusterQ.isLoading ||
    selfQ.isLoading ||
    stgGateQ.isLoading ||
    prodGateQ.isLoading

  const fixSignals: ProdFixSignal[] = [
    { label: 'Cluster · infra', signal: clusterInfra.signal, detail: clusterInfra.detail, fixScope: PROD_ENV_FIX_SCOPE },
    { label: 'STG · K8s NS', signal: k8sStgNs.signal, detail: k8sStgNs.detail, fixScope: PROD_ENV_FIX_SCOPE },
    { label: 'STG · CI/CD', signal: cicdSignal, detail: cicdDetail, fixScope: DELIVER_STG_RECOVER_SCOPE },
    {
      label: 'STG · Release gate',
      signal: stgGate.signal,
      detail: stgGate.detail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
    {
      label: 'Supply chain',
      signal: snapshot.release.signal,
      detail: snapshot.release.detail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
    {
      label: 'STG · Self-health',
      signal: selfStg.signal,
      detail: selfStg.detail,
      fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    },
    { label: 'PROD · Platform NS', signal: k8sProd.signal, detail: k8sProd.detail, fixScope: PROD_ENV_FIX_SCOPE },
    {
      label: 'PROD · Self-health',
      signal: selfProd.signal,
      detail: selfProd.detail,
      fixScope: PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    },
    {
      label: 'PROD · Release gate',
      signal: prodGate.signal,
      detail: prodGate.detail,
      fixScope: DELIVER_STG_RECOVER_SCOPE,
    },
  ]

  return { overall, stgOverall, prodOverall, isLoading, fixSignals }
}

/** Headline readiness for Satellite Deploy view — worst(STG, PROD) panels in Task CC. */
export function useSatelliteDeployOverall(enabled = true): LaunchViewOverall {
  const { snapshot, matrices, isLoading: missionLoading } = useMissionSnapshot()

  const stgBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodBusQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus', 'prod'],
    queryFn: () => fetchSatelliteBusDeep('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const prodGateQ = useQuery({
    queryKey: ['task-cc', 'trade-prod-gate'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const clusterQ = useQuery({
    queryKey: ['task-cc', 'cluster'],
    queryFn: fetchCluster,
    refetchInterval: REFETCH_MS,
    enabled,
  })

  const stgMatrix = useMemo(() => matrices.find(m => m.environment === 'stg'), [matrices])
  const prodMatrix = useMemo(() => matrices.find(m => m.environment === 'prod'), [matrices])
  const cluster = clusterQ.data
  const clusterInfra = useMemo(() => infraSignal(cluster), [cluster])
  const stgK8sNs = useMemo(() => namespacePods(cluster, STG_NS), [cluster])
  const prodK8sNs = useMemo(() => namespacePods(cluster, PROD_NS), [cluster])

  const stgDatastore = useMemo(
    () => ({ signal: datastoreEnvSignal(matrices, 'stg'), detail: datastoreDetail(matrices, 'stg') }),
    [matrices],
  )

  const prodDatastore = useMemo(
    () => ({ signal: datastoreEnvSignal(matrices, 'prod'), detail: datastoreDetail(matrices, 'prod') }),
    [matrices],
  )

  const stgBus = useMemo(() => busForEnv(stgBusQ.data, 'stg'), [stgBusQ.data])
  const prodBus = useMemo(() => busForEnv(prodBusQ.data, 'prod'), [prodBusQ.data])

  const rocket = useMemo(() => {
    // Prefer prod probe of shared gateway; fall back to stg.
    const fromProd = sharedRocketFromSocket(prodBus?.monitor.socket)
    if (prodBus != null) return fromProd
    return sharedRocketFromSocket(stgBus?.monitor.socket)
  }, [stgBus, prodBus])

  const stgTradeApis = useMemo(() => tradeApiSummary(stgMatrix), [stgMatrix])
  const prodTradeApis = useMemo(() => tradeApiSummary(prodMatrix), [prodMatrix])
  const prodGate = useMemo(() => releaseGateSignal(prodGateQ.data), [prodGateQ.data])

  const stgOverall = worst(
    clusterInfra.signal,
    stgK8sNs.signal,
    stgDatastore.signal,
    stgTradeApis.signal,
  )
  const prodOverall = worst(
    clusterInfra.signal,
    prodK8sNs.signal,
    prodDatastore.signal,
    prodTradeApis.signal,
    snapshot.tradeProd.signal,
    prodGate.signal,
  )
  const overall = worst(stgOverall, prodOverall, rocket.signal)

  const isLoading =
    missionLoading ||
    stgBusQ.isLoading ||
    prodBusQ.isLoading ||
    clusterQ.isLoading ||
    prodGateQ.isLoading

  const fixSignals: ProdFixSignal[] = [
    {
      label: 'Ground · Cluster infra',
      signal: clusterInfra.signal,
      detail: clusterInfra.detail,
      fixScope: PROD_ENV_FIX_SCOPE,
    },
    { label: 'Trade · K8s STG', signal: stgK8sNs.signal, detail: stgK8sNs.detail, fixScope: PROD_ENV_FIX_SCOPE },
    {
      label: 'Ground · PG / Redis STG',
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
    { label: 'Trade · K8s PROD', signal: prodK8sNs.signal, detail: prodK8sNs.detail, fixScope: PROD_ENV_FIX_SCOPE },
    {
      label: 'Ground · PG / Redis PROD',
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
      label: 'Rocket · IB socket',
      signal: rocket.signal,
      detail: rocket.detail,
      fixScope: SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
    },
  ]

  return {
    overall,
    stgOverall,
    prodOverall,
    rocketSignal: rocket.signal,
    rocketDetail: rocket.detail,
    isLoading,
    fixSignals,
  }
}
