import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { LaunchPad } from '@/components/control-room/LaunchPad'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  PlatformStgReleaseStrip,
  SupplyChainStrip,
  StgReleaseStrip,
} from '@/components/task-mode/ReleaseStrips'
import { DailyOpsFleetBoard } from '@/components/task-mode/DailyOpsFleetBoard'
import { DailyOpsFleetCellDetail } from '@/components/task-mode/DailyOpsFleetCellDetail'
import { DailyOpsProcessStrip } from '@/components/task-mode/DailyOpsProcessStrip'
import { DailyOpsExecutionPanel } from '@/components/task-mode/DailyOpsExecutionPanel'
import { DailyOpsOperatorPlanPanel } from '@/components/task-mode/DailyOpsOperatorPlanPanel'
import {
  DailyOpsProvider,
  type DailyOpsContextValue,
} from '@/components/task-mode/daily-ops/DailyOpsContext'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import type { DailyOpsWorkflowResult } from '@/lib/control-room/dailyOpsWorkflow'
import { TaskModeReadinessStrip } from '@/components/task-mode/TaskModeReadinessStrip'
import { LaunchLiveView } from '@/components/task-mode/LaunchLiveView'
import {
  MissionLaunchBoard,
  type CommandLane,
} from '@/components/task-mode/MissionLaunchBoard'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useDailyOpsChecklistCoverage } from '@/hooks/useDailyOpsChecklistCoverage'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { useOperateSweep } from '@/hooks/useOperateSweep'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import { type FleetCell } from '@/lib/control-room/fleetSnapshot'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import { deliveryFocusRunQueryKey } from '@/lib/delivery/deliveryFocusRun'
import { TRADE_DEPLOY_SCOPE } from '@/lib/agent/tradeDeployAgentPrompt'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'
import {
  opsDeskFocusShows,
  type OpsDeskFocus,
} from '@/lib/task-mode/opsDeskFocus'

import type { ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import type { RemediationJob } from '@/api/remediationTypes'
/** Re-exported for compatibility — release strips now live in ReleaseStrips.tsx. */
export { PlatformStgReleaseStrip }

export type OpsTaskStripsProps = {
  mode: TaskModeDef
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  onNavigate: (tabId: string) => void
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
  onDispatchRelease?: () => void
  onDispatchTradeDeploy?: () => void
  onDispatchPluginLaunch?: () => void
  releasePending?: boolean
  tradeDeployPending?: boolean
  pluginLaunchPending?: boolean
  canDispatchRelease?: boolean
  canDispatchTradeDeploy?: boolean
  canDispatchPluginLaunch?: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
  pluginLaunchDisabledReason?: string
  /**
   * When true, only render Release posture (Mission Launch board / summary rendered separately).
   * Daily Ops must not pass this — Release posture lives on Mission Launch TCC only.
   */
  promoteOnly?: boolean
  readinessCanOperate?: boolean
  onAgentFixStg?: () => void
  onAgentFixProd?: () => void
  agentFixPending?: boolean
  agentFixDisabled?: boolean
  agentFixTitle?: string
  onAgentTriage?: () => void
  agentTriagePending?: boolean
  agentTriageDisabled?: boolean
  agentTriageTitle?: string
  /** Daily Ops Fleet Desk — per-cell / primary Agent Fix */
  onFleetCellFix?: (cell: FleetCell) => void
  onFleetPrimaryCta?: () => void
  fleetAgentFixPending?: boolean
  /** Pinned Discover → Remediate → Verify → Clear process strip */
  fleetWorkflow?: DailyOpsWorkflowResult
  fleetAgentFixError?: string | null
  /** Single primary CTA path for Daily Ops Ops loop (replaces dual Verdict+Workflow). */
  onFleetWorkflowAction?: () => void
  /**
   * Operator Plane AI Fix (scope operator-plane-remediate) — Engineer CRITICAL.
   * Do NOT conflate with Checklist AI Check (daily-ops-checklist-run).
   */
  onOperatorPlanFix?: () => void
  operatorPlanFixPending?: boolean
  operatorPlanFixDisabled?: boolean
  operatorPlanFixTitle?: string
  operatorPlanFixError?: string | null
  /** Git dirty — propose commit / stash (scope git-dirty-remediate). */
  onProposeCommit?: () => void
  onProposeStash?: () => void
  proposeCommitPending?: boolean
  proposeCommitDisabled?: boolean
  proposeCommitTitle?: string
  proposeCommitError?: string | null
  /** Checklist AI Check — scope daily-ops-checklist-run (prober + dispatch gates). */
  onChecklistCheck?: () => void
  checklistCheckPending?: boolean
  checklistCheckDisabled?: boolean
  checklistCheckTitle?: string
  checklistCheckError?: string | null
  checklistCheckActive?: boolean
  checklistCheckStatusHint?: string | null
  /** Row Fix — Ops Agent for checklist item fixScope. */
  onChecklistItemFix?: (args: {
    itemId: string
    fixScope: string
    label: string
    prompt: string
  }) => void
  checklistItemFixPending?: boolean
  checklistItemFixDisabled?: boolean
  checklistItemFixTitle?: string
  checklistItemFixError?: string | null
  checklistItemFixActiveId?: string | null
  /** Wave 4.1 — open Operate Queue for checklist_dispatch Action rows. */
  onOpenOperateQueue?: (queueId?: string) => void
  /** Recent PipelineRuns for the side history column. */
  recentRuns?: DeliveryPipelineRunView[]
  recentRunsLoading?: boolean
  /** Live Go/No-Go for LaunchGateBar (Task CC). */
  launchVerdict?: LaunchVerdict
  launchCheckpoints?: LaunchCheckpoint[]
  /** Controlled Command selection shared with the Task Verdict scope line. */
  selectedCommandLane?: CommandLane
  onSelectedCommandLaneChange?: (lane: CommandLane) => void
  /** Launch-bar Agent Fix (prod remediation) — distinct from readiness strip Fix. */
  onLaunchAgentFix?: () => void
  launchAgentFixPending?: boolean
  launchAgentFixActive?: boolean
  launchAgentFixDisabled?: boolean
  launchAgentFixTitle?: string
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
  /** Expand shell Agent Execution Dock (Fix running — stay on board). */
  onExpandAgentDock?: () => void
  /** Ambient agent job — opens Launch Live View for trade-deploy / release scope. */
  ambientJobId?: string | null
  ambientJobScope?: string | null
  /** Adopt existing remediation job as ambient (Queue → Now). */
  onStartAgentJob?: (job: { id: string; scope: string; label: string }) => void
  /** Checklist auto-dispatch / related remediation jobs for Action column. */
  activeDispatchJobs?: RemediationJob[]
  /** Namespace for the mode's deliver pipeline runs (trade STG or platform). */
  pipelineRunsNamespace?: string
  /** Rocket Launch Live View post-deploy chips. */
  platformStgGate?: ReleaseGateResponse
  platformProdGate?: ReleaseGateResponse
  supplyCmsPresent?: number
  supplyCmsTotal?: number
  /** Mission Launch — trade pipeline alongside platform recentRuns. */
  tradeRecentRuns?: DeliveryPipelineRunView[]
  tradeRecentRunsLoading?: boolean
  tradePipelineRunsNamespace?: string
  satelliteLaunchVerdict?: LaunchVerdict
  satelliteLaunchCheckpoints?: LaunchCheckpoint[]
  pluginLaunchVerdict?: LaunchVerdict
  pluginLaunchCheckpoints?: LaunchCheckpoint[]
  pluginEvidence?: import('@/lib/delivery/pluginLaunchEvidence').PluginLaunchEvidence
  onSatelliteLaunchAgentFix?: () => void
  satelliteLaunchAgentFixPending?: boolean
  satelliteLaunchAgentFixActive?: boolean
  satelliteLaunchAgentFixDisabled?: boolean
  satelliteLaunchAgentFixTitle?: string
}

type SummaryRowProps = Omit<OpsTaskStripsProps, 'promoteOnly'>

/** Mission Launch summary — see MissionLaunchBoard (tabbed Vehicle | Payload lanes). */
export function OpsTaskSummaryRow(
  props: SummaryRowProps & { focus?: OpsDeskFocus },
) {
  const {
    mode,
    onNavigate,
    onDispatchRelease,
    onDispatchTradeDeploy,
    onDispatchPluginLaunch,
    releasePending,
    tradeDeployPending,
    pluginLaunchPending,
    canDispatchRelease,
    canDispatchTradeDeploy,
    canDispatchPluginLaunch,
    releaseDisabledReason,
    tradeDeployDisabledReason,
    pluginLaunchDisabledReason,
    readinessCanOperate,
    onAgentFixStg,
    onAgentFixProd,
    agentFixPending,
    agentFixDisabled,
    agentFixTitle,
    onAgentTriage,
    agentTriagePending,
    agentTriageDisabled,
    agentTriageTitle,
    recentRuns,
    recentRunsLoading,
    tradeRecentRuns,
    tradeRecentRunsLoading,
    tradePipelineRunsNamespace,
    launchVerdict,
    launchCheckpoints,
    satelliteLaunchVerdict,
    satelliteLaunchCheckpoints,
    pluginLaunchVerdict,
    pluginLaunchCheckpoints,
    pluginEvidence,
    onLaunchAgentFix,
    onSatelliteLaunchAgentFix,
    launchAgentFixPending,
    launchAgentFixActive,
    launchAgentFixDisabled,
    launchAgentFixTitle,
    satelliteLaunchAgentFixPending,
    satelliteLaunchAgentFixActive,
    satelliteLaunchAgentFixDisabled,
    satelliteLaunchAgentFixTitle,
    onOpenAgentDesk,
    onExpandAgentDock,
    ambientJobId,
    ambientJobScope,
    pipelineRunsNamespace,
    stgSmoke,
    stgGate,
    tierB,
    platformStgGate,
    platformProdGate,
    supplyCmsPresent,
    supplyCmsTotal,
    focus = 'all',
  } = props

  const [liveViewDismissed, setLiveViewDismissed] = useState(false)
  useEffect(() => {
    setLiveViewDismissed(false)
  }, [ambientJobId])

  const qc = useQueryClient()

  const openTradeRun = (run: DeliveryPipelineRunView) => {
    qc.setQueryData(deliveryFocusRunQueryKey(DELIVER_STG_PIPELINE), run.name)
    onNavigate('trade-release')
  }

  const openPlatformRun = (run: DeliveryPipelineRunView) => {
    qc.setQueryData(deliveryFocusRunQueryKey(DELIVER_PLATFORM_PIPELINE), run.name)
    onNavigate('platform-release')
  }

  const ops = mode.ops
  if (ops == null) return null

  if (mode.id === 'ops') {
    return <DailyOpsFleetDesk props={props} focus={focus} />
  }

  const isMissionLaunch = false
  const showLaunchPad =
    ops.showLaunchPad &&
    isMissionLaunch &&
    (onDispatchRelease != null || onDispatchTradeDeploy != null || onDispatchPluginLaunch != null)

  const showSatelliteLiveView =
    isMissionLaunch &&
    showLaunchPad &&
    satelliteLaunchVerdict?.kind === 'IN_FLIGHT' &&
    ambientJobId != null &&
    ambientJobId !== '' &&
    ambientJobScope === TRADE_DEPLOY_SCOPE &&
    !liveViewDismissed

  const showRocketLiveView =
    isMissionLaunch &&
    showLaunchPad &&
    launchVerdict?.kind === 'IN_FLIGHT' &&
    ambientJobId != null &&
    ambientJobId !== '' &&
    ambientJobScope === PLATFORM_RELEASE_SCOPE &&
    !liveViewDismissed

  if (showSatelliteLiveView && ambientJobId != null) {
    return (
      <LaunchLiveView
        variant="satellite"
        jobId={ambientJobId}
        taskLabel={scopeToLabel(TRADE_DEPLOY_SCOPE)}
        pipelineRuns={tradeRecentRuns}
        pipelineNamespace={tradePipelineRunsNamespace}
        stgSmoke={stgSmoke}
        stgGate={stgGate}
        tierB={tierB}
        onOpenDetail={() => onNavigate('trade-release')}
        detailLabel="Deploy Satellite →"
        onExpandAgentDock={onExpandAgentDock}
        onOpenAgentDesk={
          onOpenAgentDesk != null ? (id: string) => onOpenAgentDesk(id) : undefined
        }
        onBackToGate={() => setLiveViewDismissed(true)}
      />
    )
  }

  if (showRocketLiveView && ambientJobId != null) {
    return (
      <LaunchLiveView
        variant="rocket"
        jobId={ambientJobId}
        taskLabel={scopeToLabel(PLATFORM_RELEASE_SCOPE)}
        pipelineRuns={recentRuns}
        pipelineNamespace={pipelineRunsNamespace}
        platformStgGate={platformStgGate}
        platformProdGate={platformProdGate}
        supplyCmsPresent={supplyCmsPresent}
        supplyCmsTotal={supplyCmsTotal}
        onOpenDetail={() => onNavigate('platform-release')}
        detailLabel="Launch Rocket →"
        onExpandAgentDock={onExpandAgentDock}
        onOpenAgentDesk={
          onOpenAgentDesk != null ? (id: string) => onOpenAgentDesk(id) : undefined
        }
        onBackToGate={() => setLiveViewDismissed(true)}
      />
    )
  }

  if (
    isMissionLaunch &&
    showLaunchPad &&
    (launchVerdict != null || satelliteLaunchVerdict != null || pluginLaunchVerdict != null)
  ) {
    return (
      <MissionLaunchBoard
        onNavigate={onNavigate}
        launchVerdict={launchVerdict}
        launchCheckpoints={launchCheckpoints}
        satelliteLaunchVerdict={satelliteLaunchVerdict}
        satelliteLaunchCheckpoints={satelliteLaunchCheckpoints}
        pluginLaunchVerdict={pluginLaunchVerdict}
        pluginLaunchCheckpoints={pluginLaunchCheckpoints}
        pluginEvidence={pluginEvidence}
        onDispatchRelease={onDispatchRelease}
        onDispatchTradeDeploy={onDispatchTradeDeploy}
        onDispatchPluginLaunch={onDispatchPluginLaunch}
        releasePending={releasePending}
        tradeDeployPending={tradeDeployPending}
        pluginLaunchPending={pluginLaunchPending}
        canDispatchRelease={canDispatchRelease}
        canDispatchTradeDeploy={canDispatchTradeDeploy}
        canDispatchPluginLaunch={canDispatchPluginLaunch}
        releaseDisabledReason={releaseDisabledReason}
        tradeDeployDisabledReason={tradeDeployDisabledReason}
        pluginLaunchDisabledReason={pluginLaunchDisabledReason}
        onLaunchAgentFix={onLaunchAgentFix}
        onSatelliteLaunchAgentFix={onSatelliteLaunchAgentFix}
        launchAgentFixPending={launchAgentFixPending}
        launchAgentFixActive={launchAgentFixActive}
        launchAgentFixDisabled={launchAgentFixDisabled}
        launchAgentFixTitle={launchAgentFixTitle}
        satelliteLaunchAgentFixPending={satelliteLaunchAgentFixPending}
        satelliteLaunchAgentFixActive={satelliteLaunchAgentFixActive}
        satelliteLaunchAgentFixDisabled={satelliteLaunchAgentFixDisabled}
        satelliteLaunchAgentFixTitle={satelliteLaunchAgentFixTitle}
        onOpenAgentDesk={onOpenAgentDesk}
        onExpandAgentDock={onExpandAgentDock}
        readinessCanOperate={readinessCanOperate}
        onAgentFixStg={onAgentFixStg}
        onAgentFixProd={onAgentFixProd}
        agentFixPending={agentFixPending}
        agentFixDisabled={agentFixDisabled}
        agentFixTitle={agentFixTitle}
        onAgentTriage={onAgentTriage}
        agentTriagePending={agentTriagePending}
        agentTriageDisabled={agentTriageDisabled}
        agentTriageTitle={agentTriageTitle}
        recentRuns={recentRuns}
        recentRunsLoading={recentRunsLoading}
        tradeRecentRuns={tradeRecentRuns}
        tradeRecentRunsLoading={tradeRecentRunsLoading}
        onOpenPlatformRun={openPlatformRun}
        onOpenTradeRun={openTradeRun}
        hidePrimaryLaunch={true}
        selectedCommandLane={props.selectedCommandLane}
        onSelectedCommandLaneChange={props.onSelectedCommandLaneChange}
      />
    )
  }

  return null
}

export function DailyOpsFleetDesk({
  props,
  focus = 'all',
}: {
  props: SummaryRowProps
  /** Summary chip filter — Checklist + Fleet Board under Environment. */
  focus?: OpsDeskFocus
}) {
  const { fleet, isLoading, dataUpdatedAt } = useFleetSnapshot()
  const checklistCoverage = useDailyOpsChecklistCoverage(fleet)
  const qc = useQueryClient()
  const operateQueueQuery = useOperateQueue()
  const sweepMutation = useOperateSweep()
  const queueOpen = operateQueueQuery.data?.open.length ?? 0
  const {
    onNavigate,
    readinessCanOperate,
    onFleetCellFix,
    fleetAgentFixPending,
    fleetWorkflow,
    fleetAgentFixError,
    onFleetWorkflowAction,
    onOperatorPlanFix,
    operatorPlanFixPending,
    operatorPlanFixDisabled,
    operatorPlanFixTitle,
    operatorPlanFixError,
    onProposeCommit,
    onProposeStash,
    proposeCommitPending,
    proposeCommitDisabled,
    proposeCommitTitle,
    proposeCommitError,
    onChecklistCheck,
    checklistCheckPending,
    checklistCheckDisabled,
    checklistCheckTitle,
    checklistCheckError,
    checklistCheckActive,
    checklistCheckStatusHint,
    onChecklistItemFix,
    checklistItemFixPending,
    checklistItemFixDisabled,
    checklistItemFixTitle,
    checklistItemFixError,
    checklistItemFixActiveId,
    onOpenOperateQueue,
    onOpenAgentDesk,
    onExpandAgentDock,
    ambientJobId,
    ambientJobScope,
    onStartAgentJob,
    activeDispatchJobs,
  } = props
  const [selectedCellKey, setSelectedCellKey] = useState<string | null>(null)
  const [flashKeys, setFlashKeys] = useState<ReadonlySet<string>>(() => new Set())
  const [flashNonce, setFlashNonce] = useState(0)
  const [activeFlashStepId, setActiveFlashStepId] = useState<string | null>(null)
  const flashClearTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const selectedCell = useMemo(
    () => fleet.cells.find(c => c.key === selectedCellKey) ?? null,
    [fleet.cells, selectedCellKey],
  )

  const handleFlashChecklistStep = useCallback((stepId: string, coverageKeys: string[]) => {
    if (flashClearTimerRef.current != null) {
      clearTimeout(flashClearTimerRef.current)
      flashClearTimerRef.current = null
    }
    setActiveFlashStepId(stepId)
    setFlashKeys(new Set(coverageKeys))
    setFlashNonce(n => n + 1)
    // Scroll Fleet Board into view for long pages
    requestAnimationFrame(() => {
      document
        .querySelector('[data-daily-ops-fleet-board]')
        ?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    })
    flashClearTimerRef.current = setTimeout(() => {
      setFlashKeys(new Set())
      setActiveFlashStepId(null)
      flashClearTimerRef.current = null
    }, 2000)
  }, [])

  useEffect(() => {
    return () => {
      if (flashClearTimerRef.current != null) clearTimeout(flashClearTimerRef.current)
    }
  }, [])

  // Discover / Remediate: auto-select worst / target cell into Detail
  useEffect(() => {
    if (fleetWorkflow == null) return
    if (fleetWorkflow.activePhase !== 'discover' && fleetWorkflow.activePhase !== 'remediate') {
      return
    }
    const key = fleetWorkflow.targetCellKey
    if (key != null && key !== selectedCellKey) {
      setSelectedCellKey(key)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- avoid snapping user cell selection back on fleet object churn
  }, [fleetWorkflow?.activePhase, fleetWorkflow?.targetCellKey])

  const stripError =
    fleetWorkflow?.primaryAction.kind === 'operator-plan'
      ? (operatorPlanFixError ?? fleetAgentFixError)
      : fleetWorkflow?.primaryAction.kind === 'propose-commit'
        ? (proposeCommitError ?? fleetAgentFixError)
        : fleetAgentFixError

  const hasAmbientJob = ambientJobId != null && ambientJobId !== ''
  const itemFixStarting = checklistItemFixPending || checklistItemFixActiveId != null
  const showStartingHint =
    !hasAmbientJob &&
    (itemFixStarting ||
      checklistCheckPending ||
      (fleetAgentFixPending === true && fleetWorkflow?.activePhase === 'remediate'))
  const engineerCell =
    fleet.cells.find(c => c.role === 'engineer') ?? null
  const isChecklistAmbient =
    ambientJobScope === DAILY_OPS_CHECKLIST_RUN_SCOPE || checklistCheckActive === true

  // P3 — Fix / Also AI Fix → scroll Execution → Now into view
  useEffect(() => {
    if (!showStartingHint && !hasAmbientJob && checklistItemFixActiveId == null) return
    requestAnimationFrame(() => {
      document
        .querySelector('[data-daily-ops-execution]')
        ?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    })
  }, [showStartingHint, hasAmbientJob, checklistItemFixActiveId, ambientJobId])

  const handleVerifyReprobe = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ['cockpit'] })
    void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
  }, [qc])

  const dailyOpsContextValue: DailyOpsContextValue = {
    fleet,
    fleetWorkflow,
    isLoading,
    canOperate: readinessCanOperate,
    agentFixPending: fleetAgentFixPending,
    ambientJobId,
    ambientJobScope,
    onOpenAgentDesk,
    onExpandAgentDock,
    onStartAgentJob,
    onProposeCommit,
    onProposeStash,
    proposeCommitPending,
    proposeCommitDisabled,
    proposeCommitTitle,
    proposeCommitError,
    onChecklistCheck,
    checklistCheckPending,
    checklistCheckDisabled,
    checklistCheckTitle,
    checklistCheckError,
    checklistCheckActive: checklistCheckActive || isChecklistAmbient,
    checklistCheckStatusHint,
    onChecklistItemFix,
    checklistItemFixPending,
    checklistItemFixDisabled,
    checklistItemFixTitle,
    checklistItemFixError,
    checklistItemFixActiveId,
    onVerifyReprobe: handleVerifyReprobe,
  }

  return (
    <DailyOpsProvider value={dailyOpsContextValue}>
      <div className="flex min-w-0 max-w-full flex-col gap-3">
        {/* Ops loop + Execution — Agent focus */}
        {opsDeskFocusShows(focus, 'agent') && fleetWorkflow != null ? (
          <DailyOpsProcessStrip
            workflow={fleetWorkflow}
            agentFixError={stripError}
            showReadyHint
            onPrimaryAction={() => {
              if (fleetWorkflow.primaryAction.kind === 'propose-commit') {
                onProposeCommit?.()
                return
              }
              if (fleetWorkflow.primaryAction.kind === 'operator-plan') {
                onOperatorPlanFix?.()
                return
              }
              onFleetWorkflowAction?.()
            }}
            onSecondaryAction={
              fleetWorkflow.primaryAction.secondary?.kind === 'propose-commit'
                ? () => {
                    const label = fleetWorkflow.primaryAction.secondary?.label ?? ''
                    if (/stash/i.test(label)) onProposeStash?.()
                    else onProposeCommit?.()
                  }
                : fleetWorkflow.primaryAction.secondary?.kind === 'operator-plan'
                  ? () => onOperatorPlanFix?.()
                  : fleetWorkflow.primaryAction.secondary?.kind === 'agent-fix'
                    ? () => onFleetWorkflowAction?.()
                    : undefined
            }
            onOpenFullOperatorPlane={() => onNavigate('operator-plane')}
            onNavigate={onNavigate}
            operatorPlanFixPending={
              Boolean(operatorPlanFixPending) || Boolean(proposeCommitPending)
            }
            operatorPlanFixDisabled={
              Boolean(operatorPlanFixDisabled) || Boolean(proposeCommitDisabled)
            }
            operatorPlanFixTitle={
              fleetWorkflow.primaryAction.kind === 'propose-commit'
                ? (proposeCommitTitle ??
                  'Start git-dirty-remediate — approval required before commit/stash')
                : operatorPlanFixTitle
            }
            queueOpen={queueOpen}
            sweepQueuePending={sweepMutation.isPending}
            onSweepQueue={() => {
              sweepMutation.mutate({ auto_drain: false })
              requestAnimationFrame(() => {
                document
                  .querySelector('[data-daily-ops-execution]')
                  ?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
              })
            }}
          />
        ) : null}

        {opsDeskFocusShows(focus, 'agent') ? (
        <DailyOpsExecutionPanel
          fleetClear={fleet.fleetClear}
          remediating={
            fleetWorkflow?.activePhase === 'remediate' ||
            fleetWorkflow?.activePhase === 'verify'
          }
          showStartingHint={showStartingHint}
          primaryBlocker={fleetWorkflow?.primaryBlocker}
          primaryActionLabel={fleetWorkflow?.primaryAction.label}
          onOpsLoopAction={
            fleetWorkflow != null
              ? () => {
                  if (fleetWorkflow.primaryAction.kind === 'propose-commit') {
                    onProposeCommit?.()
                    return
                  }
                  if (fleetWorkflow.primaryAction.kind === 'operator-plan') {
                    onOperatorPlanFix?.()
                    return
                  }
                  onFleetWorkflowAction?.()
                }
              : undefined
          }
          opsLoopActionLabel={
            fleetWorkflow != null ? `${fleetWorkflow.primaryAction.label} →` : 'Ops loop →'
          }
        />
        ) : null}

        {/* Checklist | Fleet Board — Environment focus */}
        {opsDeskFocusShows(focus, 'environment') ? (
        <div className="grid min-w-0 grid-cols-1 gap-3 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,0.9fr)] xl:items-start">
          {!isLoading && (
            <div className="min-w-0 rounded-lg border border-border bg-secondary px-3 py-2">
              <DailyOpsOperatorPlanPanel
                engineerCell={engineerCell}
                coverage={checklistCoverage}
                activeFlashStepId={activeFlashStepId}
                workflowPhase={fleetWorkflow?.activePhase}
                onFlashStep={handleFlashChecklistStep}
                onOpenFullOperatorPlane={() => onNavigate('operator-plane')}
                activeDispatchJobs={activeDispatchJobs}
                onOpenDispatchJob={jobId => onOpenAgentDesk?.(jobId)}
                onOpenOperateQueue={
                  onOpenOperateQueue ??
                  ((queueId?: string) => {
                    if (queueId != null && onOpenAgentDesk != null) {
                      onOpenAgentDesk({ focusHandoffId: queueId })
                    } else if (onOpenAgentDesk != null) {
                      onOpenAgentDesk()
                    } else {
                      onNavigate('queue')
                    }
                  })
                }
                compactColumns
              />
            </div>
          )}
          <div className="flex min-w-0 flex-col gap-2">
            <DailyOpsFleetBoard
              selectedCellKey={selectedCellKey}
              coverage={checklistCoverage}
              flashKeys={flashKeys}
              flashNonce={flashNonce}
              workflowPhase={fleetWorkflow?.activePhase}
              onAgentFix={onFleetCellFix}
              onSelectCell={cell => setSelectedCellKey(cell?.key ?? null)}
              onNavigate={onNavigate}
            />
            {selectedCell != null && (
              <DailyOpsFleetCellDetail
                cell={selectedCell}
                canOperate={readinessCanOperate}
                agentFixPending={fleetAgentFixPending}
                coverage={checklistCoverage}
                dataUpdatedAt={dataUpdatedAt}
                primaryBlocker={fleetWorkflow?.primaryBlocker}
                primaryActionLabel={fleetWorkflow?.primaryAction.label}
                suppressSuggestedNext={fleetWorkflow?.activePhase === 'remediate'}
                onAgentFix={onFleetCellFix}
                onNavigate={onNavigate}
                onReprobe={handleVerifyReprobe}
                onClose={() => setSelectedCellKey(null)}
                onProposeCommit={onProposeCommit}
                onProposeStash={onProposeStash}
                proposeCommitPending={proposeCommitPending}
                proposeCommitDisabled={proposeCommitDisabled}
                proposeCommitTitle={proposeCommitTitle}
              />
            )}
          </div>
        </div>
        ) : null}
      </div>
    </DailyOpsProvider>
  )
}

export function OpsTaskStrips(props: OpsTaskStripsProps) {
  const {
    mode,
    context,
    onNavigate,
    onDispatchRelease,
    onDispatchTradeDeploy,
    onDispatchPluginLaunch,
    releasePending,
    tradeDeployPending,
    pluginLaunchPending,
    canDispatchRelease,
    canDispatchTradeDeploy,
    canDispatchPluginLaunch,
    releaseDisabledReason,
    tradeDeployDisabledReason,
    pluginLaunchDisabledReason,
    promoteOnly = false,
  } = props
  const ops = mode.ops
  if (ops == null) return null

  /** Release posture lived on Mission Launch TCC; Ops desk uses OpsDeskBoard + release tabs. */
  const promoteSection = null

  if (promoteOnly) {
    return promoteSection != null ? <div className="flex flex-col gap-3">{promoteSection}</div> : null
  }

  const isPlaybookLaunch = false

  const launchPadSection =
    ops.showLaunchPad &&
    isPlaybookLaunch &&
    (onDispatchRelease != null || onDispatchTradeDeploy != null || onDispatchPluginLaunch != null) ? (
      <OpsSection title="Mission launch">
        <LaunchPad
          variant={
            onDispatchRelease != null && onDispatchTradeDeploy != null
              ? 'both'
              : onDispatchRelease != null
                ? 'rocket-launch'
                : onDispatchTradeDeploy != null
                  ? 'satellite-deploy'
                  : 'plugin-launch'
          }
          onDispatchRelease={onDispatchRelease ?? (() => {})}
          onDispatchTradeDeploy={onDispatchTradeDeploy ?? (() => {})}
          onDispatchPluginLaunch={onDispatchPluginLaunch}
          releasePending={releasePending}
          tradeDeployPending={tradeDeployPending}
          pluginLaunchPending={pluginLaunchPending}
          canDispatchRelease={canDispatchRelease && onDispatchRelease != null}
          canDispatchTradeDeploy={canDispatchTradeDeploy && onDispatchTradeDeploy != null}
          canDispatchPluginLaunch={canDispatchPluginLaunch && onDispatchPluginLaunch != null}
          releaseDisabledReason={
            onDispatchRelease == null
              ? 'Release dispatch not wired in this view'
              : releaseDisabledReason
          }
          tradeDeployDisabledReason={
            onDispatchTradeDeploy == null
              ? 'Trade deploy dispatch not wired in this view'
              : tradeDeployDisabledReason
          }
          pluginLaunchDisabledReason={
            onDispatchPluginLaunch == null
              ? 'Plugin launch dispatch not wired in this view'
              : pluginLaunchDisabledReason
          }
          onOpenPlatformRelease={() => onNavigate('platform-release')}
          onOpenTradeDeploy={() => onNavigate('trade-release')}
          onOpenPluginRelease={() => onNavigate('plugin-release')}
        />
      </OpsSection>
    ) : null

  const supplyChainSection =
    isPlaybookLaunch && (ops.signalSource === 'supply-chain' || ops.signalSource === 'mission-launch') ? (
      <OpsSection title="Supply chain">
        <SupplyChainStrip onNavigate={onNavigate} />
      </OpsSection>
    ) : null

  const stgReleaseSection =
    isPlaybookLaunch ? (
      <OpsSection title="Trade STG deliver">
        <StgReleaseStrip context={context} onNavigate={onNavigate} />
      </OpsSection>
    ) : null

  const readinessSection =
    isPlaybookLaunch && ops.showMissionSignals ? (
      <OpsSection title="Environment readiness">
        <TaskModeReadinessStrip modeId="ops" onNavigate={onNavigate} />
      </OpsSection>
    ) : null

  if (isPlaybookLaunch) {
    return (
      <div className="flex flex-col gap-3">
        {launchPadSection}
        {supplyChainSection}
        {stgReleaseSection}
        {readinessSection}
        {promoteSection}
      </div>
    )
  }


  return (
    <div className="flex flex-col gap-3">
      {readinessSection}
    </div>
  )
}
