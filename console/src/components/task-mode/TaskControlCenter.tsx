import { useCallback, useMemo, useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { DenseTag, PageHeader } from '@bifrost/ui'
import { fetchDevAgentStatus } from '@/api/devAgent'
import {
  fetchCluster,
  fetchClusterServiceReadiness,
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSatelliteBusDeep,
  fetchSupplyChain,
  fetchStgSmoke,
  isAllSatelliteBusDeep,
} from '@/api/platform'
import { isBriefingOpened } from '@/lib/task-mode/briefingOpenedFlag'
import type {
  ClusterObservabilityResponse,
  ClusterSummary,
  MatrixResponse,
  OpsContextResponse,
} from '@/api/types'
import { OpsTaskStrips, OpsTaskSummaryRow } from '@/components/task-mode/OpsTaskStrips'
import {
  useRocketProdReadiness,
  usePromoteVerifyReadiness,
  useSatelliteDeployOverall,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { DevTaskStrips } from '@/components/task-mode/DevTaskStrips'
import { TaskPhaseProgress } from '@/components/task-mode/TaskPhaseProgress'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useDevProgramInstance } from '@/hooks/useDevProgramInstance'
import { useInlineBriefingPack } from '@/hooks/useInlineBriefingPack'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import {
  buildPlatformProdFixPrompt,
  buildTradeProdFixPrompt,
  PROD_ENV_FIX_SCOPE,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  buildSatelliteBusIngestTriagePrompt,
  SATELLITE_BUS_INGEST_TRIAGE_SCOPE,
  summarizeIngestServices,
} from '@/lib/agent/satelliteBusIngestTriagePrompt'
import { buildTradeEnvReadinessFixPrompt } from '@/lib/agent/tradeEnvReadinessFixPrompt'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import { missionStatus } from '@/lib/control-room/missionSignals'
import { collectClusterIssues } from '@/lib/cluster/collectClusterIssues'
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
  buildDailyOpsMissionFixPrompt,
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
  onOpenAgentDesk?: (jobId?: string) => void
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
  const { snapshot } = useMissionSnapshot()
  const queueQ = useOperateQueue()

  const isMissionLaunch = mode.id === 'mission-launch'
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
      (mode.id === 'daily-ops' && snapshot.missionOverall !== 'ok'),
  })

  const serviceReadinessForFixQ = useQuery({
    queryKey: ['task-cc', 'service-readiness-fix'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 20_000,
    enabled:
      isMissionLaunch ||
      (mode.id === 'daily-ops' && snapshot.missionOverall !== 'ok'),
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

  const prodFixLabel = scopeToLabel(PROD_ENV_FIX_SCOPE)

  const aiPlatformProdFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const issues = collectClusterIssues({
        summary: cluster,
        serviceReadiness,
      })
      return {
        prompt: buildPlatformProdFixPrompt({
          prodOverall: rocketProd.prodOverall,
          namespace: rocketProd.prodNamespace ?? 'bifrost-platform-prod',
          signals: rocketProd.fixSignals ?? [],
        }),
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues,
      }
    },
  })

  const aiTradeProdFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const issues = collectClusterIssues({
        summary: cluster,
        serviceReadiness,
      })
      return {
        prompt: buildTradeProdFixPrompt({
          prodOverall: satelliteProd.prodOverall,
          stgNamespace: satelliteProd.stgNamespace ?? 'bifrost-stg',
          prodNamespace: satelliteProd.prodNamespace ?? 'bifrost-prod',
          signals: [
            ...(satelliteProd.rocketBlocked && satelliteProd.rocketFixSignal != null
              ? [satelliteProd.rocketFixSignal]
              : []),
            ...(satelliteProd.fixSignals ?? []),
          ],
        }),
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues,
      }
    },
  })

  const aiTradeStgEnvFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const issues = collectClusterIssues({
        summary: cluster,
        serviceReadiness,
      })
      return {
        prompt: buildTradeEnvReadinessFixPrompt({
          env: 'stg',
          overall: satelliteDeploy.stgOverall,
          namespace: 'bifrost-stg',
          signals: stgReadinessSignals,
        }),
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues,
      }
    },
  })

  const aiTradeProdEnvFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const issues = collectClusterIssues({
        summary: cluster,
        serviceReadiness,
      })
      return {
        prompt: buildTradeEnvReadinessFixPrompt({
          env: 'prod',
          overall: satelliteDeploy.prodOverall,
          namespace: 'bifrost-prod',
          signals: prodReadinessSignals,
        }),
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues,
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
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const prompt = buildDailyOpsMissionFixPrompt(snapshot)
      if (prompt == null) {
        throw new Error('Mission is NOMINAL — nothing to fix')
      }
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      return {
        prompt,
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues: collectClusterIssues({ summary: cluster, serviceReadiness }),
      }
    },
  })

  const doneCount = phases.filter((p: TaskPhaseDef) => statuses[p.id] === 'done').length
  const loopLabel =
    mode.loopArchetype === 'ops' ? 'Ops loop' : mode.loopArchetype === 'dev' ? 'Dev loop' : 'System'

  const handleOpenPhasePage = (phase: TaskPhaseDef) => {
    if (phase.navigateTab != null) onNavigate(phase.navigateTab)
  }

  const handlePhaseFixAction = (action: TaskPhaseFixAction, _phase: TaskPhaseDef) => {
    if (action.kind === 'agent-fix') {
      if (mode.id === 'daily-ops') {
        aiDailyOpsFix.trigger()
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
    return !phases.every((p: TaskPhaseDef) => statuses[p.id] === 'done')
  }, [phases, statuses, isDevLoop])

  const [phaseOpen, setPhaseOpen] = useState(phaseDefaultOpen)
  useEffect(() => {
    setPhaseOpen(phaseDefaultOpen)
  }, [phaseDefaultOpen, mode.id])

  const headerDescription =
    mode.loopArchetype === 'dev'
      ? `Briefing → implement → deliver — playbook for ${mode.label}.`
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

  const phaseProgressBlock =
    phases.length > 0 ? (
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
            <DenseTag variant={mode.loopArchetype === 'dev' ? 'info' : 'warning'}>{loopLabel}</DenseTag>
          )
        }
      />

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
          {aiDailyOpsFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {aiDailyOpsFix.error.message}
            </OpsFeedback>
          )}
          {aiBusIngestTriage.error != null && (
            <OpsFeedback variant="error" title="Failed to start Bus Ingest Triage">
              {aiBusIngestTriage.error.message}
            </OpsFeedback>
          )}
          {mode.id === 'daily-ops' && snapshot.missionOverall !== 'ok' && (
            <OpsFeedback
              variant="warning"
              title={`Mission ${missionStatus(snapshot.missionOverall)} — fix signals before continuing`}
              actions={
                <AgentTriggerButton
                  label="Agent Fix"
                  size="xs"
                  pending={aiDailyOpsFix.isPending}
                  disabled={aiDailyOpsFix.disabled}
                  title={
                    aiDailyOpsFix.disabledReason ??
                    'Diagnose failing rocket/payload signals and remediate via Cluster · Remediate'
                  }
                  onClick={() => aiDailyOpsFix.trigger()}
                />
              }
            >
              Phase 1 stays blocked until mission signals are NOMINAL. Select step 1 in Phase progress for
              root-cause breakdown, or open Control Room for the full mission board.
            </OpsFeedback>
          )}
        </>
      )}

      {mode.loopArchetype === 'ops' &&
        (isMissionLaunch || mode.id === 'daily-ops') && (
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
            agentFixTitle="Diagnose STG/PROD trade readiness via Cluster · Remediate"
            onAgentTriage={() => aiBusIngestTriage.trigger()}
            agentTriagePending={aiBusIngestTriage.isPending}
            agentTriageDisabled={aiBusIngestTriage.disabled}
            agentTriageTitle={
              aiBusIngestTriage.disabledReason ??
              'Cross-check Socket matrix vs Rocket IB gateway (D10 safe)'
            }
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
                  'Start Cluster · Remediate focused on Platform Prod readiness')
                : undefined
            }
            satelliteLaunchAgentFixPending={isMissionLaunch ? aiTradeProdFix.isPending : false}
            satelliteLaunchAgentFixActive={isMissionLaunch ? aiTradeProdFix.isActive : false}
            satelliteLaunchAgentFixDisabled={isMissionLaunch ? aiTradeProdFix.disabled : true}
            satelliteLaunchAgentFixTitle={
              isMissionLaunch
                ? (aiTradeProdFix.disabledReason ??
                  (satelliteProd.blockKind === 'rocket'
                    ? 'Start Cluster · Remediate focused on Rocket IB bus'
                    : 'Start Cluster · Remediate focused on Trade Prod readiness'))
                : undefined
            }
            onOpenAgentDesk={() => onOpenAgentDesk?.(ambientJobId ?? undefined)}
            ambientJobId={ambientJobId}
            ambientJobScope={ambientJobScope}
            pipelineRunsNamespace={isMissionLaunch ? platformRunsQ.data?.namespace : undefined}
            platformStgGate={platformStgGateQ.data}
            platformProdGate={platformProdGateQ.data}
            supplyCmsPresent={
              supplyQ.data?.dockerfile_configmaps?.filter(c => c.present).length
            }
            supplyCmsTotal={supplyQ.data?.dockerfile_configmaps?.length}
          />
        )}

      {/* Dev: strips above phase progress (D-B / F6). Ops: phase progress first. */}
      {isDevLoop ? (
        <>
          {devStripsBlock}
          {phaseProgressBlock}
        </>
      ) : (
        <>
          {phaseProgressBlock}
          {mode.loopArchetype === 'ops' && !isMissionLaunch && (
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
              promoteOnly={mode.id === 'daily-ops'}
            />
          )}
        </>
      )}
    </div>
  )
}
