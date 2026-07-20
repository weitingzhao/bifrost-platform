import { useCallback, useMemo, useState, useEffect, useRef } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { DenseTag, PageHeader } from '@bifrost/ui'
import { fetchDevAgentStatus } from '@/api/devAgent'
import {
  fetchAgentBridge,
  fetchCluster,
  fetchClusterServiceReadiness,
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchRemediationJobs,
  fetchSatelliteBusDeep,
  fetchSupplyChain,
  fetchStgSmoke,
  isAllSatelliteBusDeep,
  startRemediation,
} from '@/api/platform'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import type {
  ClusterObservabilityResponse,
  ClusterSummary,
  MatrixResponse,
  OpsContextResponse,
} from '@/api/types'
import { OpsTaskStrips, OpsTaskSummaryRow } from '@/components/task-mode/OpsTaskStrips'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import {
  useRocketProdReadiness,
  usePromoteVerifyReadiness,
  useSatelliteDeployOverall,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { DevTaskStrips } from '@/components/task-mode/DevTaskStrips'
import { TaskPhaseProgress } from '@/components/task-mode/TaskPhaseProgress'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useDevProgramInstance } from '@/hooks/useDevProgramInstance'
import { useInlineBriefingPack } from '@/hooks/useInlineBriefingPack'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { ambientAgentBlockedReason } from '@/lib/agent/ambientAgent'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import {
  DAILY_OPS_CHECKLIST_RUN_PROMPT,
  findActiveChecklistRunJob,
} from '@/lib/control-room/checklistProgress'
import {
  buildOperatorPlaneFixPrompt,
  OPERATOR_PLANE_FIX_SCOPE,
} from '@/lib/agent/operatorPlaneFixPrompt'
import {
  buildGitDirtyRemediatePrompt,
  GIT_DIRTY_FIX_SCOPE,
} from '@/lib/agent/gitDirtyRemediatePrompt'
import {
  buildPlatformProdFixPrompt,
  buildTradeProdFixPrompt,
  pickFailingFixSignal,
  pickFixScope,
  PROD_ENV_FIX_SCOPE,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  buildClusterPackBody,
  buildDispatchedFixPrompt,
  fixScopeAgentTitle,
} from '@/lib/agent/readinessFixDispatch'
import {
  buildFleetCellFixPrompt,
  cellAllowsAgentFix,
  pickFleetFixCell,
  resolveCellFixScope,
} from '@/lib/control-room/fleetCellFix'
import { resolveDailyOpsWorkflow } from '@/lib/control-room/dailyOpsWorkflow'
import { recordChecklistRunTouch } from '@/lib/control-room/dailyOpsChecklistCoverage'
import type { FleetCell } from '@/lib/control-room/fleetSnapshot'
import { ViewerEnvBadge } from '@/components/task-mode/ViewerEnvBadge'
import {
  buildSatelliteBusIngestTriagePrompt,
  SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
  summarizeIngestServices,
} from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { buildTradeEnvReadinessFixPrompt } from '@/lib/agent/tradeEnvReadinessFixPrompt'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import { missionStatus } from '@/lib/control-room/missionSignals'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import {
  buildTradeDeployPrompt,
  TRADE_DEPLOY_SCOPE,
} from '@/lib/agent/tradeDeployAgentPrompt'
import { buildStgReleasePhases } from '@/lib/architecture/deliveryMainlineCatalog'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import {
  resolveAllTaskPhaseStatuses,
  type TaskPhaseStatusInput,
} from '@/lib/task-mode/navLens'
import {
  buildLaunchCheckpoints,
  hasDeliverInFlight,
  resolveLaunchVerdict,
} from '@/lib/task-mode/satelliteLaunchVerdict'
import {
  buildPhaseHints,
  type TaskPhaseFixAction,
} from '@/lib/task-mode/taskPhaseDiagnostics'
import { pickDeployPipelineRun } from '@/components/delivery/ReleaseStepCommandCenter'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'
import type { TaskPhaseDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'

export type TaskControlCenterProps = AmbientAgentShellProps & {
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  clusterSummary?: ClusterSummary
  clusterObservability?: ClusterObservabilityResponse
  platformHealthy?: boolean
  stgSmoke?: import('@/api/types').StgSmokeResponse
  stgGate?: import('@/api/types').ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: import('@/api/types').TierBStatusResponse
  onNavigate: (tabId: string) => void
  onOpenBriefing?: (opts?: BriefingUrlState) => void
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}

export function TaskControlCenter({
  context,
  matrices = [],
  clusterSummary,
  clusterObservability,
  platformHealthy,
  stgSmoke,
  stgGate,
  lastDeliverSucceeded,
  tierB,
  onNavigate,
  onOpenBriefing,
  onOpenPromote,
  onOpenDelivery,
  onOpenAgentDesk,
  ambientJobId,
  ambientJobScope,
  onStartAgentJob,
}: TaskControlCenterProps) {
  const { mode } = useTaskMode()
  const { canOperate } = usePlatformAuth()
  const qc = useQueryClient()
  const { fleet, snapshot, viewerEnv, viewerEnvLoading } = useFleetSnapshot()
  const queueQ = useOperateQueue()
  const [fleetFixCell, setFleetFixCell] = useState<FleetCell | null>(null)
  const fleetFixCellRef = useRef<FleetCell | null>(null)
  /** Tracks Daily Ops Agent Fix lifecycle for workflow Verify phase. */
  const dailyOpsFixStartedRef = useRef(false)
  /** Tracks Checklist AI Check (daily-ops-checklist-run) for signals invalidate. */
  const checklistCheckStartedRef = useRef(false)
  const prevAmbientJobIdRef = useRef<string | null | undefined>(undefined)
  const prevAmbientJobScopeRef = useRef<string | null | undefined>(undefined)
  const [agentJustSucceeded, setAgentJustSucceeded] = useState(false)

  const isMissionLaunch = mode.id === 'mission-launch'
  const isDailyOps = mode.id === 'daily-ops'
  const checklistCheckAmbient =
    ambientJobId != null && ambientJobScope === DAILY_OPS_CHECKLIST_RUN_SCOPE
  const [checklistJobsPollFast, setChecklistJobsPollFast] = useState(false)
  const checklistDispatchJobsQ = useQuery({
    queryKey: ['remediation', 'jobs', 'checklist-dispatch'],
    queryFn: fetchRemediationJobs,
    enabled: isDailyOps,
    refetchInterval:
      checklistCheckAmbient || checklistJobsPollFast ? 3_000 : 15_000,
  })
  const activeDispatchJobs = useMemo(() => {
    const jobs = checklistDispatchJobsQ.data?.jobs ?? []
    return jobs.filter(
      j =>
        j.status === 'running' ||
        j.actor === 'checklist-dispatch' ||
        j.scope === DAILY_OPS_CHECKLIST_RUN_SCOPE,
    )
  }, [checklistDispatchJobsQ.data?.jobs])
  const activeChecklistRunJob = useMemo(
    () => findActiveChecklistRunJob(checklistDispatchJobsQ.data?.jobs ?? []),
    [checklistDispatchJobsQ.data?.jobs],
  )
  useEffect(() => {
    setChecklistJobsPollFast(activeChecklistRunJob != null || checklistCheckAmbient)
  }, [activeChecklistRunJob, checklistCheckAmbient])
  const runnerHealthy = useMemo(() => {
    const eng = fleet.cells.find(c => c.role === 'engineer')
    if (eng == null) return false
    return eng.standards.some(
      s =>
        (s.id === 'runners-ha' || /runner/i.test(s.id) || /runner/i.test(s.label ?? '')) &&
        s.signal === 'ok',
    )
  }, [fleet.cells])
  const rocketProd = useRocketProdReadiness(isMissionLaunch)
  const satelliteProd = useSatelliteProdReadiness(isMissionLaunch)
  const promoteVerify = usePromoteVerifyReadiness(isMissionLaunch)
  const satelliteDeploy = useSatelliteDeployOverall(isMissionLaunch)

  const stgReadinessSignals = useMemo(
    () =>
      satelliteDeploy.fixSignals.filter(
        s => s.label.includes('STG') && !s.label.includes('Rocket'),
      ),
    [satelliteDeploy.fixSignals],
  )
  const prodReadinessSignals = useMemo(
    () =>
      satelliteDeploy.fixSignals.filter(
        s => s.label.includes('PROD') && !s.label.includes('Rocket'),
      ),
    [satelliteDeploy.fixSignals],
  )

  const clusterForFixQ = useQuery({
    queryKey: ['task-cc', 'cluster-fix'],
    queryFn: fetchCluster,
    refetchInterval: 20_000,
    enabled:
      isMissionLaunch ||
      (isDailyOps && !fleet.fleetClear),
  })

  const serviceReadinessForFixQ = useQuery({
    queryKey: ['task-cc', 'service-readiness-fix'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 20_000,
    enabled:
      isMissionLaunch ||
      (isDailyOps && !fleet.fleetClear),
  })

  const devProgram = useDevProgramInstance(mode)
  const programQ = devProgram

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

  const tradeProdFixSignals = useMemo(
    () => [
      ...(satelliteProd.rocketBlocked && satelliteProd.rocketFixSignal != null
        ? [satelliteProd.rocketFixSignal]
        : []),
      ...(satelliteProd.fixSignals ?? []),
    ],
    [
      satelliteProd.rocketBlocked,
      satelliteProd.rocketFixSignal,
      satelliteProd.fixSignals,
    ],
  )

  const platformProdFixScope = pickFixScope(rocketProd.fixSignals ?? [])
  const tradeProdFixScope = pickFixScope(tradeProdFixSignals)
  const tradeStgEnvFixScope = pickFixScope(stgReadinessSignals)
  const tradeProdEnvFixScope = pickFixScope(prodReadinessSignals)
  const dailyOpsTargetCell = useMemo(() => {
    if (fleetFixCell != null && cellAllowsAgentFix(fleetFixCell)) return fleetFixCell
    return pickFleetFixCell(fleet)
  }, [fleetFixCell, fleet])
  const dailyOpsFixScope = resolveCellFixScope(dailyOpsTargetCell ?? ({
    signal: 'ok',
    role: 'ground',
    env: null,
    span: true,
    key: '',
    value: '',
    detail: '',
    probePath: '',
    standards: [],
    fixScope: null,
    agentFixEnabled: false,
  } as FleetCell)) ?? PROD_ENV_FIX_SCOPE

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

  const dispatchReleaseAgent = () => {
    if (!canOperate || aiRelease.disabled || rocketVerdict.kind !== 'GO') return
    aiRelease.trigger()
  }

  const dispatchTradeDeployAgent = () => {
    if (!canOperate || aiTradeDeploy.disabled || satelliteVerdict.kind !== 'GO') return
    aiTradeDeploy.trigger()
  }

  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
    enabled: isMissionLaunch,
  })

  const platformRunsQ = useQuery({
    queryKey: ['task-cc', 'platform-runs'],
    queryFn: () => fetchPipelineRuns(DELIVER_PLATFORM_PIPELINE),
    refetchInterval: query => {
      const runs = query.state.data?.runs
      if (hasDeliverInFlight(runs)) return 5_000
      if (ambientJobId != null && ambientJobScope === PLATFORM_RELEASE_SCOPE) return 5_000
      return 20_000
    },
    enabled: isMissionLaunch || mode.id === 'rocket-build' || mode.id === 'engineer-build',
  })

  const platformStgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
    enabled: isMissionLaunch || mode.id === 'rocket-build' || mode.id === 'engineer-build',
  })

  const platformProdGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: 20_000,
    enabled: isMissionLaunch,
  })

  const tradeRunsQ = useQuery({
    queryKey: ['task-cc', 'trade-runs-detail'],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    refetchInterval: query => {
      const runs = query.state.data?.runs
      if (hasDeliverInFlight(runs)) return 5_000
      if (ambientJobId != null && ambientJobScope === TRADE_DEPLOY_SCOPE) return 5_000
      return 20_000
    },
    enabled: isMissionLaunch || mode.id === 'satellite-build' || mode.id === 'plugin-build',
  })

  const tradeGateQ = useQuery({
    queryKey: ['task-cc', 'trade-gate-detail'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
    enabled: isMissionLaunch || mode.id === 'satellite-build' || mode.id === 'plugin-build',
  })

  const smokeQ = useQuery({
    queryKey: ['task-cc', 'smoke-detail'],
    queryFn: fetchStgSmoke,
    refetchInterval: 20_000,
    enabled: isMissionLaunch || mode.id === 'satellite-build' || mode.id === 'plugin-build',
  })

  const isDevLoop = mode.loopArchetype === 'dev'
  const resolvedProgramId = devProgram.programId ?? mode.dev?.programId

  const inlineBriefingPack = useInlineBriefingPack({
    mode,
    context,
    matrices,
    clusterSummary,
    clusterObservability,
    platformHealthy,
    programId: resolvedProgramId,
    enabled: isDevLoop,
  })

  const devAgentQ = useQuery({
    queryKey: ['dev-agent', 'status'],
    queryFn: fetchDevAgentStatus,
    refetchInterval: 5000,
    enabled: isDevLoop,
  })

  const [briefingOpenedTick, setBriefingOpenedTick] = useState(0)
  const handleBriefingOpened = useCallback(() => {
    setBriefingOpenedTick(t => t + 1)
  }, [])

  const briefingOpened = useMemo(() => {
    void briefingOpenedTick
    if (!isDevLoop) return false
    return isBriefingOpened(mode.id, resolvedProgramId)
  }, [isDevLoop, mode.id, resolvedProgramId, briefingOpenedTick])

  const devAgentPhaseDone = useCallback(
    (phaseId: string): boolean => {
      const agentPhases = devAgentQ.data?.phases ?? []
      if (agentPhases.length === 0) return false
      const exact = agentPhases.find(p => p.id === phaseId)
      if (exact != null) return exact.status === 'done'
      if (phaseId === 'implement') {
        const impl = agentPhases.find(
          p =>
            p.id === 'implement' ||
            p.id.includes('implement') ||
            /implement/i.test(p.title ?? ''),
        )
        return impl != null && impl.status === 'done'
      }
      if (phaseId === 'pre-push') {
        const prePush = agentPhases.find(
          p =>
            p.id === 'pre-push' ||
            p.id.includes('pre-push') ||
            p.id.includes('verify') ||
            /pre.?push/i.test(p.title ?? ''),
        )
        if (prePush != null) return prePush.status === 'done'
        return agentPhases.length > 0 && agentPhases.every(p => p.status === 'done')
      }
      return false
    },
    [devAgentQ.data?.phases],
  )

  const statusInput = useMemo((): TaskPhaseStatusInput => {
    const platformRuns = platformRunsQ.data?.runs
    const tradeRuns = tradeRunsQ.data?.runs
    const tradeGateData = tradeGateQ.data
    const tradeSmokeOk = smokeQ.data?.reachability === 'ok'
    const platformRun = pickDeployPipelineRun(platformRuns, {
      gatePassed: platformStgGateQ.data?.result === 'pass',
    })
    const tradeRun = pickDeployPipelineRun(tradeRuns, {
      gatePassed: tradeGateData?.result === 'pass',
      smokeOk: tradeSmokeOk,
    })
    return {
      context,
      snapshot,
      supplyChain: supplyQ.data,
      stgReleasePhases: buildStgReleasePhases(context),
      operateQueueOpenCount: queueQ.data?.open.length ?? 0,
      programDetail: programQ.programDetail,
      platformStgRun: platformRun,
      platformStgGate: platformStgGateQ.data,
      platformProdGate: platformProdGateQ.data,
      tradeStgRun: tradeRun,
      tradeStgGate: tradeGateData,
      tradeStgSmokeOk: tradeSmokeOk,
      briefingOpened,
      devAgentPhaseDone: isDevLoop ? devAgentPhaseDone : undefined,
      fleetAgentFixAvailable:
        isDailyOps && dailyOpsTargetCell != null && cellAllowsAgentFix(dailyOpsTargetCell),
    }
  }, [
    context,
    snapshot,
    supplyQ.data,
    queueQ.data,
    programQ.programDetail,
    platformRunsQ.data,
    platformStgGateQ.data,
    platformProdGateQ.data,
    tradeRunsQ.data,
    tradeGateQ.data,
    smokeQ.data,
    briefingOpened,
    isDevLoop,
    devAgentPhaseDone,
    isDailyOps,
    dailyOpsTargetCell,
  ])

  const phases = mode.phases ?? []
  const statuses = useMemo(
    () => resolveAllTaskPhaseStatuses(mode.id, statusInput),
    [mode.id, statusInput],
  )

  const phaseHints = useMemo(
    () => buildPhaseHints(mode.id, phases, statuses, statusInput),
    [mode.id, phases, statuses, statusInput],
  )

  const aiDailyOpsFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: dailyOpsFixScope,
    label: scopeToLabel(dailyOpsFixScope),
    buildRequest: async () => {
      const cell =
        fleetFixCellRef.current ??
        dailyOpsTargetCell ??
        pickFleetFixCell(fleet)
      if (cell == null || !cellAllowsAgentFix(cell)) {
        throw new Error('Fleet is clear or selected cell is not Agent-Fixable')
      }
      const scope = resolveCellFixScope(cell) ?? PROD_ENV_FIX_SCOPE
      const cellPrompt = buildFleetCellFixPrompt(cell, fleet)
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const [supply, smoke] = await Promise.all([fetchSupplyChain(), fetchStgSmoke()])
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals: [
            {
              label: `${cell.role} · ${cell.env ?? 'span'}`,
              signal: cell.signal === 'unavailable' ? 'unknown' : cell.signal,
              detail: cell.detail,
              fixScope: scope,
            },
          ],
          clusterFallbackPrompt: cellPrompt,
          extras: { supply, stgSmoke: smoke },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const aiOperatorPlaneFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: OPERATOR_PLANE_FIX_SCOPE,
    label: scopeToLabel(OPERATOR_PLANE_FIX_SCOPE),
    buildRequest: async () => {
      const bridge = await fetchAgentBridge()
      return { prompt: buildOperatorPlaneFixPrompt(bridge) }
    },
  })

  const gitDirtyIntentRef = useRef<'commit' | 'stash'>('commit')
  const aiGitDirtyFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: GIT_DIRTY_FIX_SCOPE,
    label: scopeToLabel(GIT_DIRTY_FIX_SCOPE),
    buildRequest: async () => {
      const bridge = await fetchAgentBridge()
      const base = buildGitDirtyRemediatePrompt(bridge)
      const intent = gitDirtyIntentRef.current
      const extra =
        intent === 'stash'
          ? [
              '',
              '## Operator intent: STASH (not commit)',
              'Prefer git_stash after request_operator_approval. Do not git_commit unless operator changes mind.',
            ].join('\n')
          : [
              '',
              '## Operator intent: PROPOSE COMMIT',
              'Draft commit_message → request_operator_approval → git_commit. Stash only if operator rejects commit and asks to stash.',
            ].join('\n')
      return { prompt: `${base}${extra}` }
    },
  })

  /** Checklist AI Check — scope daily-ops-checklist-run (not Operator Plane Fix). */
  const aiChecklistCheck = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: DAILY_OPS_CHECKLIST_RUN_SCOPE,
    label: scopeToLabel(DAILY_OPS_CHECKLIST_RUN_SCOPE),
    buildRequest: () => ({ prompt: DAILY_OPS_CHECKLIST_RUN_PROMPT }),
  })

  const handleFleetCellFix = (cell: FleetCell) => {
    fleetFixCellRef.current = cell
    setFleetFixCell(cell)
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    // Mark standards in this cell as a real checklist run (vs dry-run coverage).
    recordChecklistRunTouch(cell)
    aiDailyOpsFix.trigger()
  }

  const handleOperatorPlanFix = () => {
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    const engineerCell = fleet.cells.find(c => c.role === 'engineer')
    if (engineerCell != null) recordChecklistRunTouch(engineerCell)
    aiOperatorPlaneFix.trigger()
  }

  const handleProposeCommit = () => {
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    gitDirtyIntentRef.current = 'commit'
    const engineerCell = fleet.cells.find(c => c.role === 'engineer')
    if (engineerCell != null) recordChecklistRunTouch(engineerCell)
    aiGitDirtyFix.trigger()
  }

  const handleProposeStash = () => {
    dailyOpsFixStartedRef.current = true
    setAgentJustSucceeded(false)
    gitDirtyIntentRef.current = 'stash'
    const engineerCell = fleet.cells.find(c => c.role === 'engineer')
    if (engineerCell != null) recordChecklistRunTouch(engineerCell)
    aiGitDirtyFix.trigger()
  }

  const handleChecklistCheck = () => {
    checklistCheckStartedRef.current = true
    aiChecklistCheck.trigger()
  }

  const checklistItemFixRef = useRef<{
    itemId: string
    scope: string
    label: string
    prompt: string
  } | null>(null)
  const [checklistItemFixActiveId, setChecklistItemFixActiveId] = useState<string | null>(
    null,
  )

  const aiChecklistItemFix = useMutation({
    mutationFn: async () => {
      const r = checklistItemFixRef.current
      if (r == null) throw new Error('No checklist item selected for Fix')
      return startRemediation({ scope: r.scope, prompt: r.prompt })
    },
    onSuccess: job => {
      const r = checklistItemFixRef.current
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
      onStartAgentJob?.({
        id: job.id,
        scope: r?.scope ?? job.scope ?? 'checklist-item-fix',
        label: r?.label ?? scopeToLabel(r?.scope ?? job.scope ?? 'checklist-item-fix'),
      })
    },
  })

  // Keep row/section highlight until ambient job ends (not merely until mutate settles).
  useEffect(() => {
    if (ambientJobId == null) setChecklistItemFixActiveId(null)
  }, [ambientJobId])

  const checklistItemFixBlocked = ambientAgentBlockedReason(
    canOperate,
    ambientJobId,
    onStartAgentJob,
  )

  const handleChecklistItemFix = (args: {
    itemId: string
    fixScope: string
    label: string
    prompt: string
  }) => {
    checklistItemFixRef.current = {
      itemId: args.itemId,
      scope: args.fixScope,
      label: args.label,
      prompt: args.prompt,
    }
    setChecklistItemFixActiveId(args.itemId)
    setAgentJustSucceeded(false)
    aiChecklistItemFix.mutate()
  }

  const checklistCheckActive =
    isDailyOps &&
    (aiChecklistCheck.isPending ||
      checklistCheckAmbient ||
      activeChecklistRunJob != null)

  const checklistCheckDisabled =
    aiChecklistCheck.disabled || !runnerHealthy

  const checklistCheckTitle = !runnerHealthy
    ? 'Remediation runner not healthy — check Engineer · runners-ha'
    : (aiChecklistCheck.disabledReason ??
      'AI Check: daily-ops-checklist-run probe → report_checklist_signals (not Operator Plane Fix)')

  const handleFleetPrimaryCta = () => {
    const cta = fleet.verdict.primaryCta
    if (cta.kind === 'navigate' && cta.tabId != null) {
      onNavigate(cta.tabId)
      return
    }
    if (cta.kind === 'agent-fix') {
      const cell =
        (cta.cellKey != null ? fleet.cells.find(c => c.key === cta.cellKey) : null) ??
        pickFleetFixCell(fleet)
      if (cell != null) handleFleetCellFix(cell)
    }
  }

  // Ambient job ended after Daily Ops Agent Fix → Verify (re-probe) only on success
  // Checklist AI Check done/failed → refresh signals / KPIs / jobs
  useEffect(() => {
    const prevId = prevAmbientJobIdRef.current
    const prevScope = prevAmbientJobScopeRef.current
    prevAmbientJobIdRef.current = ambientJobId
    prevAmbientJobScopeRef.current = ambientJobScope

    if (prevId != null && ambientJobId == null) {
      if (prevScope === DAILY_OPS_CHECKLIST_RUN_SCOPE || checklistCheckStartedRef.current) {
        checklistCheckStartedRef.current = false
        void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
        void qc.invalidateQueries({ queryKey: ['checklist', 'kpis'] })
        void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
        void qc.invalidateQueries({ queryKey: ['cockpit'] })
      }
      if (dailyOpsFixStartedRef.current && prevScope !== DAILY_OPS_CHECKLIST_RUN_SCOPE) {
        // Only enter Verify when the job actually succeeded (not failed/cancelled)
        const jobsCaches = [
          qc.getQueryData<{ jobs: { id: string; status: string }[] }>(['remediation', 'jobs']),
          qc.getQueryData<{ jobs: { id: string; status: string }[] }>([
            'remediation',
            'jobs',
            'checklist-dispatch',
          ]),
        ]
        const ended = jobsCaches
          .flatMap(c => c?.jobs ?? [])
          .find(j => j.id === prevId)
        if (ended?.status === 'done') {
          setAgentJustSucceeded(true)
        }
        void qc.invalidateQueries({ queryKey: ['cockpit'] })
        void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
        void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      }
    }
  }, [ambientJobId, ambientJobScope, qc])

  useEffect(() => {
    if (fleet.fleetClear) {
      setAgentJustSucceeded(false)
      dailyOpsFixStartedRef.current = false
    }
  }, [fleet.fleetClear])

  const dailyOpsAgentPending =
    isDailyOps &&
    (aiDailyOpsFix.isPending ||
      aiOperatorPlaneFix.isPending ||
      aiGitDirtyFix.isPending ||
      (ambientJobId != null && dailyOpsFixStartedRef.current))

  const dailyOpsWorkflow = useMemo(() => {
    if (!isDailyOps) return null
    return resolveDailyOpsWorkflow({
      fleet,
      agentPending: dailyOpsAgentPending,
      agentJustSucceeded,
      queueOpen: queueQ.data?.open.length ?? 0,
    })
  }, [
    isDailyOps,
    fleet,
    dailyOpsAgentPending,
    agentJustSucceeded,
    queueQ.data?.open.length,
  ])

  const handleFleetWorkflowAction = () => {
    if (dailyOpsWorkflow == null) return
    const action = dailyOpsWorkflow.primaryAction
    if (action.kind === 'agent-fix') {
      const cell =
        (action.cellKey != null ? fleet.cells.find(c => c.key === action.cellKey) : null) ??
        pickFleetFixCell(fleet)
      if (cell != null && cellAllowsAgentFix(cell)) {
        handleFleetCellFix(cell)
      }
      return
    }
    if (action.kind === 'operator-plan') {
      handleOperatorPlanFix()
      return
    }
    if (action.kind === 'propose-commit') {
      handleProposeCommit()
      return
    }
    if (action.kind === 'manual-next') {
      const hint = action.manualHint ?? action.label
      void navigator.clipboard?.writeText(hint).then(
        () => {
          /* copied — strip title already shows the next step */
        },
        () => {
          /* clipboard may be denied; label still visible */
        },
      )
      if (action.tabId != null) {
        // Stay on TCC; Operator Plan panel is already inline. Full page only via escape link.
      }
      return
    }
    if (action.kind === 'view-agent') {
      onOpenAgentDesk?.(ambientJobId ?? undefined)
      return
    }
    if (action.kind === 'navigate' || action.kind === 'clear-queue') {
      if (action.tabId != null) onNavigate(action.tabId)
      return
    }
    if (action.kind === 'ai-check' || action.kind === 'run-check') {
      // Discover AI Check + Clear idle re-check → daily-ops-checklist-run
      handleChecklistCheck()
      return
    }
    if (action.kind === 'verify') {
      void qc.invalidateQueries({ queryKey: ['cockpit'] })
    }
  }

  const doneCount = phases.filter((p: TaskPhaseDef) => statuses[p.id] === 'done').length
  const loopLabel =
    mode.loopArchetype === 'ops' ? 'Ops loop' : mode.loopArchetype === 'dev' ? 'Dev loop' : 'System'

  const handleOpenPhasePage = (phase: TaskPhaseDef) => {
    if (phase.navigateTab != null) onNavigate(phase.navigateTab)
  }

  const handlePhaseFixAction = (action: TaskPhaseFixAction, _phase: TaskPhaseDef) => {
    if (action.kind === 'agent-fix') {
      if (mode.id === 'daily-ops') {
        const cell = pickFleetFixCell(fleet)
        if (cell != null) handleFleetCellFix(cell)
        return
      }
      if (isMissionLaunch) {
        if (rocketProd.prodBlocked) {
          aiPlatformProdFix.trigger()
          return
        }
        aiTradeProdFix.trigger()
        return
      }
      return
    }
    if (action.tabId != null) onNavigate(action.tabId)
  }

  const showLaunchPad = mode.ops?.showLaunchPad === true

  const satelliteVerdictInput = useMemo(
    () => ({
      mode: 'satellite' as const,
      canOperate,
      prodBlocked: satelliteProd.prodBlocked,
      blockKind: satelliteProd.blockKind ?? undefined,
      rocketDetail: satelliteProd.rocketDetail,
      rocketLabel: missionStatus(satelliteProd.rocketSignal),
      rocketSignal: satelliteProd.rocketSignal,
      tradeProdLabel: missionStatus(satelliteProd.tradeProdOverall),
      tradeProdSignal: satelliteProd.tradeProdOverall,
      promoteSignal: promoteVerify.promoteSignal,
      promoteDetail: promoteVerify.promoteDetail,
      deliverInFlight: hasDeliverInFlight(tradeRunsQ.data?.runs),
      agentInFlight: ambientJobId != null && ambientJobScope === TRADE_DEPLOY_SCOPE,
    }),
    [
      canOperate,
      satelliteProd.prodBlocked,
      satelliteProd.blockKind,
      satelliteProd.rocketDetail,
      satelliteProd.rocketSignal,
      satelliteProd.tradeProdOverall,
      promoteVerify.promoteSignal,
      promoteVerify.promoteDetail,
      tradeRunsQ.data?.runs,
      ambientJobId,
      ambientJobScope,
    ],
  )

  const satelliteVerdict = useMemo(
    () => resolveLaunchVerdict(satelliteVerdictInput),
    [satelliteVerdictInput],
  )
  const satelliteCheckpoints = useMemo(
    () => buildLaunchCheckpoints(satelliteVerdictInput),
    [satelliteVerdictInput],
  )

  const rocketVerdictInput = useMemo(
    () => ({
      mode: 'rocket' as const,
      canOperate,
      prodBlocked: rocketProd.prodBlocked,
      tradeProdLabel: missionStatus(rocketProd.prodOverall),
      tradeProdSignal: rocketProd.prodOverall,
      promoteSignal: promoteVerify.promoteSignal,
      promoteDetail: promoteVerify.promoteDetail,
      deliverInFlight: hasDeliverInFlight(platformRunsQ.data?.runs),
      agentInFlight: ambientJobId != null && ambientJobScope === PLATFORM_RELEASE_SCOPE,
    }),
    [
      canOperate,
      rocketProd.prodBlocked,
      rocketProd.prodOverall,
      promoteVerify.promoteSignal,
      promoteVerify.promoteDetail,
      platformRunsQ.data?.runs,
      ambientJobId,
      ambientJobScope,
    ],
  )

  const rocketVerdict = useMemo(() => resolveLaunchVerdict(rocketVerdictInput), [rocketVerdictInput])
  const rocketCheckpoints = useMemo(
    () => buildLaunchCheckpoints(rocketVerdictInput),
    [rocketVerdictInput],
  )

  const releaseDispatchAllowed =
    showLaunchPad && !aiRelease.disabled && rocketVerdict.kind === 'GO'
  const tradeDeployDispatchAllowed =
    showLaunchPad && !aiTradeDeploy.disabled && satelliteVerdict.kind === 'GO'
  const releaseDisabledReason =
    rocketVerdict.kind !== 'GO'
      ? rocketVerdict.disabledReason
      : aiRelease.disabledReason
  const tradeDeployDisabledReason =
    satelliteVerdict.kind !== 'GO'
      ? satelliteVerdict.disabledReason
      : aiTradeDeploy.disabledReason

  const phaseDefaultOpen = useMemo(() => {
    if (phases.length === 0) return false
    if (isDevLoop) return false
    // Daily Ops — phase playbook is reference only (A7); default collapsed
    if (isDailyOps) return false
    return !phases.every((p: TaskPhaseDef) => statuses[p.id] === 'done')
  }, [phases, statuses, isDevLoop, isDailyOps])

  const [phaseOpen, setPhaseOpen] = useState(phaseDefaultOpen)
  useEffect(() => {
    setPhaseOpen(phaseDefaultOpen)
  }, [phaseDefaultOpen, mode.id])

  const headerDescription =
    mode.loopArchetype === 'dev'
      ? `Briefing → implement → deliver — playbook for ${mode.label}.`
      : isDailyOps
        ? `Ops loop — Discover → Remediate → Verify → Clear — Fleet Desk is health ground truth.`
        : mode.loopArchetype === 'ops'
          ? `${mode.label} · ${loopLabel} — live Go/No-Go, recent launches, and playbook reference.`
          : `${mode.label} · ${loopLabel}`

  const phaseProgressHint = isDevLoop
    ? phaseOpen
      ? 'Open — Dev playbook checklist'
      : 'Collapsed — Dev playbook checklist'
    : phaseOpen
      ? 'Open — not live Go/No-Go'
      : 'Collapsed — not live Go/No-Go'

  const phaseProgressCaption = isDevLoop
    ? 'Playbook phase status — Briefing → implement → deliver → sign-off'
    : 'Historical phase checklist — not live environment health'

  /** Daily Ops: no phase strip — Help · reference lives inside Ops loop (DailyOpsProcessStrip). */
  const phaseProgressBlock = isDailyOps ? null : phases.length > 0 ? (
    <details
      className="rounded-lg border border-border bg-card px-3 py-1.5"
      open={phaseOpen}
      onToggle={e => setPhaseOpen((e.currentTarget as HTMLDetailsElement).open)}
    >
      <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-meta)] font-semibold">Phase progress</span>
          <DenseTag variant="neutral" className="text-[9px]">
            Playbook
          </DenseTag>
          <DenseTag variant="neutral" className="text-[9px]">
            {doneCount}/{phases.length} complete
          </DenseTag>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {phaseProgressHint}
          </span>
        </div>
      </summary>
      <p className="m-0 mb-1.5 mt-1.5 text-[var(--text-dense-caption)] text-muted-foreground">
        {phaseProgressCaption}
      </p>
      <TaskPhaseProgress
        phases={phases}
        statuses={statuses}
        hints={phaseHints}
        onOpenFullPage={handleOpenPhasePage}
        onFixAction={handlePhaseFixAction}
      />
      {isMissionLaunch && (satelliteProd.prodBlocked || rocketProd.prodBlocked) && (
        <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-warning">
          Live readiness blocked — playbook Done does not clear release
        </p>
      )}
    </details>
  ) : null

  const devStripsBlock = isDevLoop ? (
    <DevTaskStrips
      mode={mode}
      canOperate={canOperate}
      programDetail={devProgram.programDetail}
      programLoading={devProgram.programLoading}
      programError={devProgram.programError}
      resolvedProgramId={resolvedProgramId}
      createPending={devProgram.createPending}
      onCreateProgram={devProgram.ensureProgram}
      onCreateNewInstance={() => devProgram.createNewInstance({ instanceLabel: mode.label })}
      onNavigate={onNavigate}
      inlineBriefingPack={inlineBriefingPack}
      onOpenFullBriefing={onOpenBriefing}
      onBriefingOpened={handleBriefingOpened}
      devAgentStatus={devAgentQ.data}
      devAgentLoading={devAgentQ.isLoading}
      phases={phases}
      phaseStatuses={statuses}
    />
  ) : null

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Task Control Center"
        description={headerDescription}
        actions={
          mode.loopArchetype === 'system' ? undefined : (
            <div className="flex flex-wrap items-center gap-1.5">
              {(isDailyOps || mode.loopArchetype === 'ops') && (
                <ViewerEnvBadge viewerEnv={viewerEnv} isLoading={viewerEnvLoading} />
              )}
              <DenseTag variant={mode.loopArchetype === 'dev' ? 'info' : 'warning'}>{loopLabel}</DenseTag>
            </div>
          )
        }
      />

      {isDailyOps && aiDailyOpsFix.error != null && (
        <OpsFeedback variant="error" title="Failed to start Agent Fix">
          {aiDailyOpsFix.error.message}
        </OpsFeedback>
      )}

      {mode.loopArchetype === 'ops' && showLaunchPad && (
        <>
          {isMissionLaunch && !canOperate && (
            <OpsFeedback variant="warning" title="Authenticate as operator to run Launch Pad agents">
              Use the header auth control before starting release or trade-deploy Agent tasks.
            </OpsFeedback>
          )}
          {aiRelease.error != null && (
            <OpsFeedback variant="error" title="Failed to start AI Release">
              {aiRelease.error.message}
            </OpsFeedback>
          )}
          {aiTradeDeploy.error != null && (
            <OpsFeedback variant="error" title="Failed to start Deploy Satellite agent">
              {aiTradeDeploy.error.message}
            </OpsFeedback>
          )}
          {aiPlatformProdFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {aiPlatformProdFix.error.message}
            </OpsFeedback>
          )}
          {aiTradeProdFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {aiTradeProdFix.error.message}
            </OpsFeedback>
          )}
          {aiBusIngestTriage.error != null && (
            <OpsFeedback variant="error" title="Failed to start Bus Ingest Triage">
              {aiBusIngestTriage.error.message}
            </OpsFeedback>
          )}
        </>
      )}

      {mode.loopArchetype === 'ops' &&
        (isMissionLaunch || isDailyOps) && (
          <OpsTaskSummaryRow
            mode={mode}
            context={context}
            matrices={matrices}
            stgSmoke={stgSmoke}
            stgGate={stgGate}
            lastDeliverSucceeded={lastDeliverSucceeded}
            tierB={tierB}
            onNavigate={onNavigate}
            onOpenPromote={onOpenPromote}
            onOpenDelivery={onOpenDelivery}
            onDispatchRelease={showLaunchPad ? dispatchReleaseAgent : undefined}
            onDispatchTradeDeploy={showLaunchPad ? dispatchTradeDeployAgent : undefined}
            releasePending={aiRelease.isPending}
            tradeDeployPending={aiTradeDeploy.isPending}
            canDispatchRelease={releaseDispatchAllowed}
            canDispatchTradeDeploy={tradeDeployDispatchAllowed}
            releaseDisabledReason={releaseDisabledReason}
            tradeDeployDisabledReason={tradeDeployDisabledReason}
            readinessCanOperate={canOperate}
            onAgentFixStg={() => aiTradeStgEnvFix.trigger()}
            onAgentFixProd={() => aiTradeProdEnvFix.trigger()}
            agentFixPending={aiTradeStgEnvFix.isPending || aiTradeProdEnvFix.isPending}
            agentFixDisabled={!canOperate}
            agentFixTitle={fixScopeAgentTitle(
              tradeStgEnvFixScope,
              scopeToLabel(tradeStgEnvFixScope),
              pickFailingFixSignal(stgReadinessSignals)?.label ??
                pickFailingFixSignal(prodReadinessSignals)?.label,
            )}
            onAgentTriage={() => aiBusIngestTriage.trigger()}
            agentTriagePending={aiBusIngestTriage.isPending}
            agentTriageDisabled={aiBusIngestTriage.disabled}
            agentTriageTitle={
              aiBusIngestTriage.disabledReason ??
              'Cross-check Socket matrix vs Rocket IB gateway (D10 safe)'
            }
            onFleetCellFix={isDailyOps ? handleFleetCellFix : undefined}
            onFleetPrimaryCta={isDailyOps ? handleFleetPrimaryCta : undefined}
            fleetAgentFixPending={isDailyOps ? dailyOpsAgentPending : undefined}
            fleetWorkflow={dailyOpsWorkflow ?? undefined}
            fleetAgentFixError={isDailyOps ? (aiDailyOpsFix.error?.message ?? null) : undefined}
            onFleetWorkflowAction={isDailyOps ? handleFleetWorkflowAction : undefined}
            onOperatorPlanFix={isDailyOps ? handleOperatorPlanFix : undefined}
            operatorPlanFixPending={isDailyOps ? aiOperatorPlaneFix.isPending : undefined}
            operatorPlanFixDisabled={isDailyOps ? aiOperatorPlaneFix.disabled : undefined}
            operatorPlanFixTitle={
              isDailyOps
                ? (aiOperatorPlaneFix.disabledReason ??
                  'Start Operator · Remediate with current bridge probe')
                : undefined
            }
            operatorPlanFixError={
              isDailyOps ? (aiOperatorPlaneFix.error?.message ?? null) : undefined
            }
            onProposeCommit={isDailyOps ? handleProposeCommit : undefined}
            onProposeStash={isDailyOps ? handleProposeStash : undefined}
            proposeCommitPending={isDailyOps ? aiGitDirtyFix.isPending : undefined}
            proposeCommitDisabled={isDailyOps ? aiGitDirtyFix.disabled : undefined}
            proposeCommitTitle={
              isDailyOps
                ? (aiGitDirtyFix.disabledReason ??
                  'Start git-dirty-remediate — approval required before commit/stash')
                : undefined
            }
            onChecklistCheck={isDailyOps ? handleChecklistCheck : undefined}
            checklistCheckPending={isDailyOps ? aiChecklistCheck.isPending : undefined}
            checklistCheckDisabled={isDailyOps ? checklistCheckDisabled : undefined}
            checklistCheckTitle={isDailyOps ? checklistCheckTitle : undefined}
            checklistCheckError={
              isDailyOps ? (aiChecklistCheck.error?.message ?? null) : undefined
            }
            checklistCheckActive={isDailyOps ? checklistCheckActive : undefined}
            checklistCheckStatusHint={
              isDailyOps ? (activeChecklistRunJob?.phase ?? null) : undefined
            }
            onChecklistItemFix={isDailyOps ? handleChecklistItemFix : undefined}
            checklistItemFixPending={isDailyOps ? aiChecklistItemFix.isPending : undefined}
            checklistItemFixDisabled={
              isDailyOps
                ? checklistItemFixBlocked != null || !runnerHealthy
                : undefined
            }
            checklistItemFixTitle={
              isDailyOps
                ? !runnerHealthy
                  ? 'Remediation runner not healthy — check Engineer · runners-ha'
                  : (checklistItemFixBlocked ??
                    'Start Ops Agent Fix for this checklist item (not Cursor Ask for AI)')
                : undefined
            }
            checklistItemFixError={
              isDailyOps ? (aiChecklistItemFix.error?.message ?? null) : undefined
            }
            checklistItemFixActiveId={isDailyOps ? checklistItemFixActiveId : undefined}
            recentRuns={isMissionLaunch ? platformRunsQ.data?.runs : undefined}
            recentRunsLoading={isMissionLaunch ? platformRunsQ.isLoading : false}
            tradeRecentRuns={isMissionLaunch ? tradeRunsQ.data?.runs : undefined}
            tradeRecentRunsLoading={isMissionLaunch ? tradeRunsQ.isLoading : false}
            tradePipelineRunsNamespace={isMissionLaunch ? tradeRunsQ.data?.namespace : undefined}
            launchVerdict={isMissionLaunch ? rocketVerdict : undefined}
            launchCheckpoints={isMissionLaunch ? rocketCheckpoints : undefined}
            satelliteLaunchVerdict={isMissionLaunch ? satelliteVerdict : undefined}
            satelliteLaunchCheckpoints={isMissionLaunch ? satelliteCheckpoints : undefined}
            onLaunchAgentFix={isMissionLaunch ? () => aiPlatformProdFix.trigger() : undefined}
            onSatelliteLaunchAgentFix={isMissionLaunch ? () => aiTradeProdFix.trigger() : undefined}
            launchAgentFixPending={isMissionLaunch ? aiPlatformProdFix.isPending : false}
            launchAgentFixActive={isMissionLaunch ? aiPlatformProdFix.isActive : false}
            launchAgentFixDisabled={isMissionLaunch ? aiPlatformProdFix.disabled : true}
            launchAgentFixTitle={
              isMissionLaunch
                ? (aiPlatformProdFix.disabledReason ??
                  fixScopeAgentTitle(
                    platformProdFixScope,
                    scopeToLabel(platformProdFixScope),
                    pickFailingFixSignal(rocketProd.fixSignals ?? [])?.label,
                  ))
                : undefined
            }
            satelliteLaunchAgentFixPending={isMissionLaunch ? aiTradeProdFix.isPending : false}
            satelliteLaunchAgentFixActive={isMissionLaunch ? aiTradeProdFix.isActive : false}
            satelliteLaunchAgentFixDisabled={isMissionLaunch ? aiTradeProdFix.disabled : true}
            satelliteLaunchAgentFixTitle={
              isMissionLaunch
                ? (aiTradeProdFix.disabledReason ??
                  fixScopeAgentTitle(
                    tradeProdFixScope,
                    scopeToLabel(tradeProdFixScope),
                    pickFailingFixSignal(tradeProdFixSignals)?.label,
                  ))
                : undefined
            }
            onOpenAgentDesk={arg =>
              onOpenAgentDesk?.(arg ?? ambientJobId ?? undefined)
            }
            ambientJobId={ambientJobId}
            ambientJobScope={ambientJobScope}
            onStartAgentJob={isDailyOps ? onStartAgentJob : undefined}
            activeDispatchJobs={isDailyOps ? activeDispatchJobs : undefined}
            pipelineRunsNamespace={isMissionLaunch ? platformRunsQ.data?.namespace : undefined}
            platformStgGate={platformStgGateQ.data}
            platformProdGate={platformProdGateQ.data}
            supplyCmsPresent={
              supplyQ.data?.dockerfile_configmaps?.filter(c => c.present).length
            }
            supplyCmsTotal={supplyQ.data?.dockerfile_configmaps?.length}
          />
        )}

      {/* Dev: strips above phase. Daily Ops: Ops loop (Help inside) → Agent → Checklist|Board → Operate (no Release). Mission Launch: board + Release posture. */}
      {isDevLoop ? (
        <>
          {devStripsBlock}
          {phaseProgressBlock}
        </>
      ) : (
        <>
          {phaseProgressBlock}
          {isMissionLaunch && (
            <OpsTaskStrips
              mode={mode}
              context={context}
              matrices={matrices}
              stgSmoke={stgSmoke}
              stgGate={stgGate}
              lastDeliverSucceeded={lastDeliverSucceeded}
              tierB={tierB}
              onNavigate={onNavigate}
              onOpenPromote={onOpenPromote}
              onOpenDelivery={onOpenDelivery}
              onDispatchRelease={showLaunchPad ? dispatchReleaseAgent : undefined}
              onDispatchTradeDeploy={showLaunchPad ? dispatchTradeDeployAgent : undefined}
              releasePending={aiRelease.isPending}
              tradeDeployPending={aiTradeDeploy.isPending}
              canDispatchRelease={releaseDispatchAllowed}
              canDispatchTradeDeploy={tradeDeployDispatchAllowed}
              releaseDisabledReason={releaseDisabledReason}
              tradeDeployDisabledReason={tradeDeployDisabledReason}
              promoteOnly
            />
          )}
        </>
      )}
    </div>
  )
}
