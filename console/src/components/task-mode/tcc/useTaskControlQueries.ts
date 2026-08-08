import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchPipelineRuns, fetchSupplyChain } from '@/api/delivery'
import { fetchReleaseGate, fetchStgSmoke } from '@/api/promote'
import { fetchRemediationJobs } from '@/api/remediation'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { pickDeployPipelineRun } from '@/components/delivery/ReleaseStepCommandCenter'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import { TRADE_DEPLOY_SCOPE } from '@/lib/agent/tradeDeployAgentPrompt'
import { findActiveChecklistRunJob } from '@/lib/control-room/checklistProgress'
import type { ProgramDetailResponse } from '@/api/programsTypes'
import type { FleetCell, FleetSnapshot } from '@/lib/control-room/fleetSnapshot'
import { missionStatus, type MissionSnapshot } from '@/lib/control-room/missionSignals'
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
import { buildPhaseHints } from '@/lib/task-mode/taskPhaseDiagnostics'
import type { TaskModeDef } from '@/lib/task-mode/types'
import {
  useRocketProdReadiness,
  usePromoteVerifyReadiness,
  useSatelliteDeployOverall,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { cellAllowsAgentFix } from '@/lib/control-room/fleetCellFix'

/**
 * Task Control Center data layer — cluster/delivery/promote/smoke useQuery
 * cluster plus phase-status + launch-verdict derivation.
 */
export function useTaskControlQueries({
  mode,
  isMissionLaunch,
  isDailyOps,
  isDevLoop,
  fleet,
  fleetClear,
  ambientJobId,
  ambientJobScope,
  context,
  snapshot,
  operateQueueOpenCount,
  programDetail,
  briefingOpened,
  devAgentPhaseDone,
  dailyOpsTargetCell,
  canOperate,
  rocketProd,
  satelliteProd,
  promoteVerify,
  satelliteDeploy,
}: {
  mode: TaskModeDef
  isMissionLaunch: boolean
  isDailyOps: boolean
  isDevLoop: boolean
  fleet: FleetSnapshot
  fleetClear: boolean
  ambientJobId?: string | null
  ambientJobScope?: string | null
  context?: OpsContextResponse
  snapshot: MissionSnapshot
  operateQueueOpenCount: number
  programDetail?: ProgramDetailResponse
  briefingOpened: boolean
  devAgentPhaseDone: (phaseId: string) => boolean
  dailyOpsTargetCell: FleetCell | null
  canOperate: boolean
  rocketProd: ReturnType<typeof useRocketProdReadiness>
  satelliteProd: ReturnType<typeof useSatelliteProdReadiness>
  promoteVerify: ReturnType<typeof usePromoteVerifyReadiness>
  satelliteDeploy: ReturnType<typeof useSatelliteDeployOverall>
}) {
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
    enabled: isMissionLaunch || (isDailyOps && !fleetClear),
  })

  const serviceReadinessForFixQ = useQuery({
    queryKey: ['task-cc', 'service-readiness-fix'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 20_000,
    enabled: isMissionLaunch || (isDailyOps && !fleetClear),
  })

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
    enabled: isMissionLaunch || isDevLoop,
  })

  const platformStgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
    enabled: isMissionLaunch || isDevLoop,
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
    enabled: isMissionLaunch || isDevLoop,
  })

  const tradeGateQ = useQuery({
    queryKey: ['task-cc', 'trade-gate-detail'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
    enabled: isMissionLaunch || isDevLoop,
  })

  const smokeQ = useQuery({
    queryKey: ['task-cc', 'smoke-detail'],
    queryFn: fetchStgSmoke,
    refetchInterval: 20_000,
    enabled: isMissionLaunch || isDevLoop,
  })

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
      operateQueueOpenCount,
      programDetail,
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
    operateQueueOpenCount,
    programDetail,
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

  return {
    checklistCheckAmbient,
    activeDispatchJobs,
    activeChecklistRunJob,
    runnerHealthy,
    fleetClear,
    stgReadinessSignals,
    prodReadinessSignals,
    clusterForFixQ,
    serviceReadinessForFixQ,
    supplyQ,
    platformRunsQ,
    platformStgGateQ,
    platformProdGateQ,
    tradeRunsQ,
    tradeGateQ,
    smokeQ,
    statusInput,
    phases,
    statuses,
    phaseHints,
    satelliteVerdict,
    satelliteCheckpoints,
    rocketVerdict,
    rocketCheckpoints,
  }
}
