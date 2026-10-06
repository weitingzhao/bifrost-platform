import { useMemo, useRef, useState } from 'react'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import {
  useRocketProdReadiness,
  usePromoteVerifyReadiness,
  useSatelliteDeployOverall,
  useSatelliteProdReadiness,
} from '@/components/task-mode/readiness/hooks'
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
import { useTaskMode } from '@/lib/task-mode/useTaskMode'
import type { TaskModeId, TaskPhaseDef } from '@/lib/task-mode/types'
import type { TaskPhaseFixAction } from '@/lib/task-mode/taskPhaseDiagnostics'
import { useTaskControlQueries } from '@/components/task-mode/tcc/useTaskControlQueries'
import { useChecklistItemFix } from '@/components/task-mode/tcc/useChecklistItemFix'
import { TaskControlCenterView } from '@/components/task-mode/tcc/TaskControlCenterView'

export type TaskControlCenterProps = AmbientAgentShellProps & {
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  onNavigate: (tabId: string) => void
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
}

/** Wiring shell — data hooks + View composition. */
export function TaskControlCenter({
  context,
  matrices = [],
  stgSmoke,
  stgGate,
  lastDeliverSucceeded,
  tierB,
  onNavigate,
  onModeChange,
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

  const isOps = mode.id === 'ops'
  /** Launch Pad path kept compiled but unused after Launch merged into Ops. */
  const isMissionLaunch = false
  const isDailyOps = isOps
  const isSystem = mode.id === 'system'
  const showLaunchPad = false

  /** Release Focus on Daily Ops needs the same readiness/gate data Mission Launch used. */
  const releaseReadinessEnabled = isMissionLaunch || isDailyOps
  const rocketProd = useRocketProdReadiness(releaseReadinessEnabled)
  const satelliteProd = useSatelliteProdReadiness(releaseReadinessEnabled)
  const promoteVerify = usePromoteVerifyReadiness(releaseReadinessEnabled)
  const satelliteDeploy = useSatelliteDeployOverall(releaseReadinessEnabled)
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

  const q = useTaskControlQueries({
    mode, isMissionLaunch, isDailyOps, fleet,
    fleetClear: fleet.fleetClear, ambientJobId, ambientJobScope, context, snapshot,
    operateQueueOpenCount: queueQ.data?.open.length ?? 0,
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
    !agents.aiRelease.disabled && q.rocketVerdict.kind === 'GO'
  const tradeDeployDispatchAllowed =
    !agents.aiTradeDeploy.disabled && q.satelliteVerdict.kind === 'GO'
  const pluginLaunchDispatchAllowed =
    !agents.aiPluginLaunch.disabled && pluginVerdict.kind === 'GO'
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
    mode.loopArchetype === 'ops'
      ? 'Ops loop'
      : mode.loopArchetype === 'analysis'
        ? 'Analysis'
        : 'System'

  const [phaseFixUnavailableHint, setPhaseFixUnavailableHint] = useState<string | null>(null)

  const handleOpenPhasePage = (phase: TaskPhaseDef) => {
    if (phase.navigateTab != null) onNavigate(phase.navigateTab)
  }
  const handlePhaseFixAction = (action: TaskPhaseFixAction, _phase: TaskPhaseDef) => {
    if (action.kind === 'agent-fix') {
      if (mode.id === 'ops') {
        setPhaseFixUnavailableHint(null)
        const cell = pickFleetFixCell(fleet)
        if (cell != null) fix.handleFleetCellFix(cell)
        return
      }
      setPhaseFixUnavailableHint(
        `Agent Fix is not available in ${mode.label}. Switch to Ops.`,
      )
      return
    }
    setPhaseFixUnavailableHint(null)
    if (action.tabId != null) onNavigate(action.tabId)
  }

  const phaseDefaultOpen = useMemo(() => {
    if (q.phases.length === 0 || isDailyOps) return false
    return !q.phases.every((p: TaskPhaseDef) => q.statuses[p.id] === 'done')
  }, [q.phases, q.statuses, isDailyOps])

  const headerDescription = (() => {
    if (isOps) {
      return 'Ops loop — Discover → Remediate → Deploy → Patrol → Clear — Fleet + Queue + Patrol on one desk.'
    }
    if (mode.id === 'analysis') {
      return mode.description
    }
    if (mode.loopArchetype === 'ops') {
      return `${mode.label} · ${loopLabel} — live Go/No-Go and playbook reference.`
    }
    return `${mode.label} · ${loopLabel}`
  })()

  return (
    <TaskControlCenterView
      mode={mode}
      isMissionLaunch={isMissionLaunch}
      isDailyOps={isDailyOps}
      isSystem={isSystem}
      operateQueueOpen={queueQ.data?.open.length ?? 0}
      onModeChange={onModeChange}
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
      onNavigate={onNavigate}
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
