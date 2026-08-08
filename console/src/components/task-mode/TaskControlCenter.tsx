import { useMemo, useRef, useState } from 'react'
import type { ClusterObservabilityResponse, ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import {
  useRocketProdReadiness,
  usePromoteVerifyReadiness,
  useSatelliteDeployOverall,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { useDevModeController } from '@/components/task-mode/DevModeController'
import { useMissionLaunchFixAgents } from '@/components/task-mode/useMissionLaunchFixAgents'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useIbGatewayLiveProbe } from '@/hooks/useIbGatewayLiveProbe'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { PLUGIN_LAUNCH_SCOPE } from '@/lib/agent/pluginLaunchAgentPrompt'
import {
  buildPluginLaunchCheckpoints,
  resolvePluginLaunchVerdict,
} from '@/lib/task-mode/pluginLaunchVerdict'
import { readPluginLaunchEvidence } from '@/lib/delivery/pluginLaunchEvidence'
import {
  cellAllowsAgentFix,
  pickFleetFixCell,
  resolveCellFixScope,
} from '@/lib/control-room/fleetCellFix'
import type { FleetCell } from '@/lib/control-room/fleetSnapshot'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'
import type { TaskPhaseDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import type { TaskPhaseFixAction } from '@/lib/task-mode/taskPhaseDiagnostics'
import { useTaskControlQueries } from '@/components/task-mode/tcc/useTaskControlQueries'
import { useChecklistItemFix } from '@/components/task-mode/tcc/useChecklistItemFix'
import { TaskControlCenterView } from '@/components/task-mode/tcc/TaskControlCenterView'

export type TaskControlCenterProps = AmbientAgentShellProps & {
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  clusterSummary?: ClusterSummary
  clusterObservability?: ClusterObservabilityResponse
  platformHealthy?: boolean
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  onNavigate: (tabId: string) => void
  onOpenBriefing?: (opts?: BriefingUrlState) => void
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}

/** Wiring shell — data hooks + View composition. */
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
  onExpandAgentDock,
  ambientJobId,
  ambientJobScope,
  onStartAgentJob,
}: TaskControlCenterProps) {
  const { mode } = useTaskMode()
  const { canOperate } = usePlatformAuth()
  const { fleet, snapshot, viewerEnv, viewerEnvLoading } = useFleetSnapshot()
  const queueQ = useOperateQueue()
  const [fleetFixCell, setFleetFixCell] = useState<FleetCell | null>(null)
  const fleetFixCellRef = useRef<FleetCell | null>(null)

  const isMissionLaunch = mode.id === 'mission-launch'
  const isDailyOps = mode.id === 'daily-ops'
  const isDevLoop = mode.loopArchetype === 'dev'
  const showLaunchPad = mode.ops?.showLaunchPad === true

  const rocketProd = useRocketProdReadiness(isMissionLaunch)
  const satelliteProd = useSatelliteProdReadiness(isMissionLaunch)
  const promoteVerify = usePromoteVerifyReadiness(isMissionLaunch)
  const satelliteDeploy = useSatelliteDeployOverall(isMissionLaunch)
  // This probe accepts only an interval; call it unconditionally to preserve hook order.
  const liveProbe = useIbGatewayLiveProbe()
  const pluginEvidence = readPluginLaunchEvidence()

  const dailyOpsTargetCell = useMemo(() => {
    if (fleetFixCell != null && cellAllowsAgentFix(fleetFixCell)) return fleetFixCell
    return pickFleetFixCell(fleet)
  }, [fleetFixCell, fleet])
  const emptyCell = {
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
  } as FleetCell
  const dailyOpsFixScope =
    resolveCellFixScope(dailyOpsTargetCell ?? emptyCell) ?? PROD_ENV_FIX_SCOPE

  const {
    devProgram,
    resolvedProgramId,
    inlineBriefingPack,
    devAgentQ,
    briefingOpened,
    handleBriefingOpened,
    devAgentPhaseDone,
  } = useDevModeController({
    mode, isDevLoop, context, matrices, clusterSummary, clusterObservability, platformHealthy,
  })

  const q = useTaskControlQueries({
    mode, isMissionLaunch, isDailyOps, isDevLoop, fleet,
    fleetClear: fleet.fleetClear, ambientJobId, ambientJobScope, context, snapshot,
    operateQueueOpenCount: queueQ.data?.open.length ?? 0,
    programDetail: devProgram.programDetail, briefingOpened, devAgentPhaseDone,
    dailyOpsTargetCell, canOperate, rocketProd, satelliteProd, promoteVerify, satelliteDeploy,
  })

  const agents = useMissionLaunchFixAgents({
    isMissionLaunch, canOperate, ambientJobId, onStartAgentJob, context, matrices, stgSmoke, tierB,
    rocketProd, satelliteProd, satelliteDeploy,
    stgReadinessSignals: q.stgReadinessSignals, prodReadinessSignals: q.prodReadinessSignals,
    clusterForFixQ: q.clusterForFixQ, serviceReadinessForFixQ: q.serviceReadinessForFixQ,
  })
  const pluginAgentInFlight =
    agents.aiPluginLaunch.isPending || ambientJobScope === PLUGIN_LAUNCH_SCOPE
  const pluginVerdict = resolvePluginLaunchVerdict({
    canOperate,
    status: liveProbe.status,
    evidence: pluginEvidence,
    agentInFlight: pluginAgentInFlight,
  })
  const pluginCheckpoints = buildPluginLaunchCheckpoints({
    canOperate,
    status: liveProbe.status,
    evidence: pluginEvidence,
    agentInFlight: pluginAgentInFlight,
  })

  const fix = useChecklistItemFix({
    isDailyOps, canOperate, ambientJobId, ambientJobScope, onStartAgentJob, onNavigate, onOpenAgentDesk,
    onExpandAgentDock,
    fleet, setFleetFixCell, fleetFixCellRef, dailyOpsTargetCell, dailyOpsFixScope,
    clusterForFixQ: q.clusterForFixQ, serviceReadinessForFixQ: q.serviceReadinessForFixQ,
    runnerHealthy: q.runnerHealthy, checklistCheckAmbient: q.checklistCheckAmbient,
    activeChecklistRunJob: q.activeChecklistRunJob,
    operateQueueOpenCount: queueQ.data?.open.length ?? 0,
  })

  const dispatchReleaseAgent = () => {
    if (!canOperate || agents.aiRelease.disabled || q.rocketVerdict.kind !== 'GO') return
    agents.aiRelease.trigger()
  }
  const dispatchTradeDeployAgent = () => {
    if (!canOperate || agents.aiTradeDeploy.disabled || q.satelliteVerdict.kind !== 'GO') return
    agents.aiTradeDeploy.trigger()
  }
  const dispatchPluginLaunchAgent = () => {
    if (!canOperate || agents.aiPluginLaunch.disabled || pluginVerdict.kind !== 'GO') return
    agents.aiPluginLaunch.trigger()
  }

  const releaseDispatchAllowed =
    showLaunchPad && !agents.aiRelease.disabled && q.rocketVerdict.kind === 'GO'
  const tradeDeployDispatchAllowed =
    showLaunchPad && !agents.aiTradeDeploy.disabled && q.satelliteVerdict.kind === 'GO'
  const pluginLaunchDispatchAllowed =
    showLaunchPad && !agents.aiPluginLaunch.disabled && pluginVerdict.kind === 'GO'
  const releaseDisabledReason =
    q.rocketVerdict.kind !== 'GO' ? q.rocketVerdict.disabledReason : agents.aiRelease.disabledReason
  const tradeDeployDisabledReason =
    q.satelliteVerdict.kind !== 'GO'
      ? q.satelliteVerdict.disabledReason
      : agents.aiTradeDeploy.disabledReason
  const pluginLaunchDisabledReason =
    pluginVerdict.kind !== 'GO'
      ? pluginVerdict.disabledReason
      : agents.aiPluginLaunch.disabledReason

  const doneCount = q.phases.filter((p: TaskPhaseDef) => q.statuses[p.id] === 'done').length
  const loopLabel =
    mode.loopArchetype === 'ops' ? 'Ops loop' : mode.loopArchetype === 'dev' ? 'Dev loop' : 'System'

  const [phaseFixUnavailableHint, setPhaseFixUnavailableHint] = useState<string | null>(null)

  const handleOpenPhasePage = (phase: TaskPhaseDef) => {
    if (phase.navigateTab != null) onNavigate(phase.navigateTab)
  }
  const handlePhaseFixAction = (action: TaskPhaseFixAction, _phase: TaskPhaseDef) => {
    if (action.kind === 'agent-fix') {
      if (mode.id === 'daily-ops') {
        setPhaseFixUnavailableHint(null)
        const cell = pickFleetFixCell(fleet)
        if (cell != null) fix.handleFleetCellFix(cell)
        return
      }
      if (isMissionLaunch) {
        setPhaseFixUnavailableHint(null)
        if (rocketProd.prodBlocked) agents.aiPlatformProdFix.trigger()
        else agents.aiTradeProdFix.trigger()
        return
      }
      setPhaseFixUnavailableHint(
        `Agent Fix is not available in ${mode.label}. Switch to Daily Ops or Launch.`,
      )
      return
    }
    setPhaseFixUnavailableHint(null)
    if (action.tabId != null) onNavigate(action.tabId)
  }

  const phaseDefaultOpen = useMemo(() => {
    if (q.phases.length === 0 || isDevLoop || isDailyOps) return false
    return !q.phases.every((p: TaskPhaseDef) => q.statuses[p.id] === 'done')
  }, [q.phases, q.statuses, isDevLoop, isDailyOps])

  const headerDescription =
    mode.loopArchetype === 'dev'
      ? `Briefing → implement → deliver — playbook for ${mode.label}.`
      : isDailyOps
        ? `Ops loop — Discover → Remediate → Verify → Clear — Fleet Desk is health ground truth.`
        : mode.loopArchetype === 'ops'
          ? `${mode.label} · ${loopLabel} — live Go/No-Go, recent launches, and playbook reference.`
          : `${mode.label} · ${loopLabel}`

  return (
    <TaskControlCenterView
      mode={mode}
      isMissionLaunch={isMissionLaunch}
      isDailyOps={isDailyOps}
      isDevLoop={isDevLoop}
      canOperate={canOperate}
      loopLabel={loopLabel}
      headerDescription={headerDescription}
      viewerEnv={viewerEnv}
      viewerEnvLoading={viewerEnvLoading}
      showLaunchPad={showLaunchPad}
      phases={q.phases}
      statuses={q.statuses}
      phaseHints={q.phaseHints}
      doneCount={doneCount}
      phaseDefaultOpen={phaseDefaultOpen}
      rocketProd={rocketProd}
      satelliteProd={satelliteProd}
      onOpenPhasePage={handleOpenPhasePage}
      onPhaseFixAction={handlePhaseFixAction}
      devProgram={devProgram}
      resolvedProgramId={resolvedProgramId}
      inlineBriefingPack={inlineBriefingPack}
      onNavigate={onNavigate}
      onOpenBriefing={onOpenBriefing}
      handleBriefingOpened={handleBriefingOpened}
      devAgentQ={devAgentQ}
      context={context}
      matrices={matrices}
      stgSmoke={stgSmoke}
      stgGate={stgGate}
      lastDeliverSucceeded={lastDeliverSucceeded}
      tierB={tierB}
      onOpenPromote={onOpenPromote}
      onOpenDelivery={onOpenDelivery}
      onOpenAgentDesk={onOpenAgentDesk}
      onExpandAgentDock={onExpandAgentDock}
      ambientJobId={ambientJobId}
      ambientJobScope={ambientJobScope}
      onStartAgentJob={onStartAgentJob}
      q={q}
      agents={agents}
      fix={fix}
      dispatchReleaseAgent={dispatchReleaseAgent}
      dispatchTradeDeployAgent={dispatchTradeDeployAgent}
      dispatchPluginLaunchAgent={dispatchPluginLaunchAgent}
      releaseDispatchAllowed={releaseDispatchAllowed}
      tradeDeployDispatchAllowed={tradeDeployDispatchAllowed}
      pluginLaunchDispatchAllowed={pluginLaunchDispatchAllowed}
      releaseDisabledReason={releaseDisabledReason}
      tradeDeployDisabledReason={tradeDeployDisabledReason}
      pluginLaunchDisabledReason={pluginLaunchDisabledReason}
      pluginLaunchVerdict={pluginVerdict}
      pluginLaunchCheckpoints={pluginCheckpoints}
      pluginEvidence={pluginEvidence}
      missionOverall={snapshot.missionOverall}
      phaseFixUnavailableHint={phaseFixUnavailableHint}
    />
  )
}
