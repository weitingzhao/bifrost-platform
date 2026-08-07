import { useQuery } from '@tanstack/react-query'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchSatelliteBusDeep, isAllSatelliteBusDeep } from '@/api/core'
import { fetchStgSmoke } from '@/api/promote'
import { fetchSupplyChain } from '@/api/delivery'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { TierBStatusResponse } from '@/api/deliveryTypes'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import {
  buildPlatformProdFixPrompt,
  buildTradeProdFixPrompt,
  pickFixScope,
  PROD_ENV_FIX_SCOPE,
  type ProdFixSignal,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  buildClusterPackBody,
  buildDispatchedFixPrompt,
} from '@/lib/agent/readinessFixDispatch'
import {
  buildSatelliteBusIngestTriagePrompt,
  SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
  summarizeIngestServices,
} from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { buildTradeEnvReadinessFixPrompt } from '@/lib/agent/tradeEnvReadinessFixPrompt'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import { buildTradeDeployPrompt, TRADE_DEPLOY_SCOPE } from '@/lib/agent/tradeDeployAgentPrompt'
import {
  buildPluginLaunchPrompt,
  PLUGIN_LAUNCH_SCOPE,
} from '@/lib/agent/pluginLaunchAgentPrompt'
import {
  readPluginLaunchEvidence,
  readPluginLaunchStore,
} from '@/lib/delivery/pluginLaunchEvidence'
import type {
  useRocketProdReadiness,
  useSatelliteDeployOverall,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'

import type { StgSmokeResponse } from '@/api/deliveryTypes'
/**
 * Mission Launch "AI Fix" ambient agent tasks — Rocket/Satellite release
 * dispatch + Platform/Trade Prod & STG env fix + satellite bus ingest triage.
 *
 * Extracted from TaskControlCenter to keep the buildRequest closures (each
 * fetching cluster/service-readiness/supply/smoke packs) out of the main
 * component body.
 */
export function useMissionLaunchFixAgents({
  isMissionLaunch,
  canOperate,
  ambientJobId,
  onStartAgentJob,
  context,
  matrices,
  stgSmoke,
  tierB,
  rocketProd,
  satelliteProd,
  satelliteDeploy,
  stgReadinessSignals,
  prodReadinessSignals,
  clusterForFixQ,
  serviceReadinessForFixQ,
}: AmbientAgentShellProps & {
  isMissionLaunch: boolean
  canOperate: boolean
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  tierB?: TierBStatusResponse
  rocketProd: ReturnType<typeof useRocketProdReadiness>
  satelliteProd: ReturnType<typeof useSatelliteProdReadiness>
  satelliteDeploy: ReturnType<typeof useSatelliteDeployOverall>
  stgReadinessSignals: ProdFixSignal[]
  prodReadinessSignals: ProdFixSignal[]
  clusterForFixQ: ReturnType<typeof useQuery<Awaited<ReturnType<typeof fetchCluster>>>>
  serviceReadinessForFixQ: ReturnType<
    typeof useQuery<Awaited<ReturnType<typeof fetchClusterServiceReadiness>>>
  >
}) {
  const tradeProdFixSignals: ProdFixSignal[] = [
    ...(satelliteProd.rocketBlocked && satelliteProd.rocketFixSignal != null
      ? [satelliteProd.rocketFixSignal]
      : []),
    ...(satelliteProd.fixSignals ?? []),
  ]

  const platformProdFixScope = pickFixScope(rocketProd.fixSignals ?? [])
  const tradeProdFixScope = pickFixScope(tradeProdFixSignals)
  const tradeStgEnvFixScope = pickFixScope(stgReadinessSignals)
  const tradeProdEnvFixScope = pickFixScope(prodReadinessSignals)

  const aiRelease = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PLATFORM_RELEASE_SCOPE,
    label: scopeToLabel(PLATFORM_RELEASE_SCOPE),
    buildRequest: () => {
      const spineNote =
        context?.focus?.headline != null ? `Spine focus: ${context.focus.headline}\n\n` : ''
      return { prompt: `${spineNote}${PLATFORM_RELEASE_AGENT_PROMPT}` }
    },
  })

  const aiTradeDeploy = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: TRADE_DEPLOY_SCOPE,
    label: scopeToLabel(TRADE_DEPLOY_SCOPE),
    buildRequest: () => ({
      prompt: buildTradeDeployPrompt({
        matrices,
        stgSmoke,
        tierB,
      }),
    }),
  })

  const aiPluginLaunch = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PLUGIN_LAUNCH_SCOPE,
    label: scopeToLabel(PLUGIN_LAUNCH_SCOPE),
    buildRequest: () => {
      const store = readPluginLaunchStore()
      const seat = store.selectedTarget === 'market-data' ? store.selectedSeat : 'dev'
      return {
        prompt: buildPluginLaunchPrompt({
          target: store.selectedTarget,
          seat,
          evidence: readPluginLaunchEvidence(store.selectedTarget, seat),
          operatorSurface: 'Mission Launch TCC',
        }),
      }
    },
  })

  const aiPlatformProdFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: platformProdFixScope,
    label: scopeToLabel(platformProdFixScope),
    buildRequest: async () => {
      const signals = rocketProd.fixSignals ?? []
      const scope = pickFixScope(signals)
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const [supply, smoke] = await Promise.all([fetchSupplyChain(), fetchStgSmoke()])
      const fallback = buildPlatformProdFixPrompt({
        prodOverall: rocketProd.prodOverall,
        namespace: rocketProd.prodNamespace ?? 'bifrost-platform-prod',
        signals,
      })
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals,
          clusterFallbackPrompt: fallback,
          extras: {
            supply,
            stgSmoke: smoke,
            pipeline: 'bifrost-deliver-platform',
          },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const aiTradeProdFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: tradeProdFixScope,
    label: scopeToLabel(tradeProdFixScope),
    buildRequest: async () => {
      const signals = tradeProdFixSignals
      const scope = pickFixScope(signals)
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const [supply, smoke] = await Promise.all([fetchSupplyChain(), fetchStgSmoke()])
      const fallback = buildTradeProdFixPrompt({
        prodOverall: satelliteProd.prodOverall,
        stgNamespace: satelliteProd.stgNamespace ?? 'bifrost-stg',
        prodNamespace: satelliteProd.prodNamespace ?? 'bifrost-prod',
        signals,
      })
      let busTriagePrompt: string | undefined
      if (scope === SATELLITE_BUS_INGEST_TRIAGE_SCOPE) {
        const data = await fetchSatelliteBusDeep('stg')
        const bus =
          data != null && isAllSatelliteBusDeep(data)
            ? data.buses.find(b => b.environment === 'stg')
            : data
        const ingestHeadline = summarizeIngestServices(bus?.ingest.services ?? []).headline
        const socket = bus?.monitor.socket
        const socketRows = socket
          ? [
              socket.massive,
              socket.ib_ingestor,
              socket.ib_account_agent,
              socket.ib_operator,
              socket.platform_ib_gateway,
            ]
          : []
        const socketOk = socketRows.filter(r => r?.reachability === 'ok').length
        const socketHeadline =
          socket == null
            ? 'Monitor socket block unavailable'
            : `${socketOk}/${socketRows.length} socket components ok`
        busTriagePrompt = buildSatelliteBusIngestTriagePrompt({
          env: 'stg',
          namespace: 'bifrost-stg',
          ingestHeadline,
          socketHeadline,
          busReachability: bus?.reachability,
        })
      }
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals,
          clusterFallbackPrompt: fallback,
          extras: { supply, stgSmoke: smoke, pipeline: 'bifrost-deliver-stg' },
          busTriagePrompt,
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const aiTradeStgEnvFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: tradeStgEnvFixScope,
    label: scopeToLabel(tradeStgEnvFixScope),
    buildRequest: async () => {
      const signals = stgReadinessSignals
      const scope = pickFixScope(signals)
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const [supply, smoke] = await Promise.all([fetchSupplyChain(), fetchStgSmoke()])
      const fallback = buildTradeEnvReadinessFixPrompt({
        env: 'stg',
        overall: satelliteDeploy.stgOverall,
        namespace: 'bifrost-stg',
        signals,
      })
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals,
          clusterFallbackPrompt: fallback,
          extras: { supply, stgSmoke: smoke, pipeline: 'bifrost-deliver-stg' },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const aiTradeProdEnvFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: tradeProdEnvFixScope,
    label: scopeToLabel(tradeProdEnvFixScope),
    buildRequest: async () => {
      const signals = prodReadinessSignals
      const scope = pickFixScope(signals)
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const [supply, smoke] = await Promise.all([fetchSupplyChain(), fetchStgSmoke()])
      const fallback = buildTradeEnvReadinessFixPrompt({
        env: 'prod',
        overall: satelliteDeploy.prodOverall,
        namespace: 'bifrost-prod',
        signals,
      })
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals,
          clusterFallbackPrompt: fallback,
          extras: { supply, stgSmoke: smoke, pipeline: 'bifrost-deliver-stg' },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const stgBusForTriageQ = useQuery({
    queryKey: ['task-cc', 'satellite-bus-triage', 'stg'],
    queryFn: () => fetchSatelliteBusDeep('stg'),
    refetchInterval: 20_000,
    enabled: isMissionLaunch,
  })

  const aiBusIngestTriage = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
    label: scopeToLabel(SATELLITE_BUS_INGEST_TRIAGE_SCOPE),
    buildRequest: async () => {
      const data = stgBusForTriageQ.data ?? (await fetchSatelliteBusDeep('stg'))
      const bus =
        data != null && isAllSatelliteBusDeep(data)
          ? data.buses.find(b => b.environment === 'stg')
          : data
      const ingestHeadline = summarizeIngestServices(bus?.ingest.services ?? []).headline
      const socket = bus?.monitor.socket
      const socketRows = socket
        ? [
            socket.massive,
            socket.ib_ingestor,
            socket.ib_account_agent,
            socket.ib_operator,
            socket.platform_ib_gateway,
          ]
        : []
      const socketOk = socketRows.filter(r => r?.reachability === 'ok').length
      const socketHeadline =
        socket == null
          ? 'Monitor socket block unavailable'
          : `${socketOk}/${socketRows.length} socket components ok`
      return {
        prompt: buildSatelliteBusIngestTriagePrompt({
          env: 'stg',
          namespace: 'bifrost-stg',
          ingestHeadline,
          socketHeadline,
          busReachability: bus?.reachability,
        }),
      }
    },
  })

  return {
    aiRelease,
    aiTradeDeploy,
    aiPluginLaunch,
    aiPlatformProdFix,
    aiTradeProdFix,
    aiTradeStgEnvFix,
    aiTradeProdEnvFix,
    aiBusIngestTriage,
    platformProdFixScope,
    tradeProdFixScope,
    tradeStgEnvFixScope,
    tradeProdEnvFixScope,
    tradeProdFixSignals,
  }
}
