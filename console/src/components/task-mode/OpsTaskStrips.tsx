import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
  DenseTag,
} from '@bifrost/ui'
import { ChevronRight } from 'lucide-react'
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
import { MissionLaunchBoard } from '@/components/task-mode/MissionLaunchBoard'
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
import { PromoteCutoverStrip } from '@/components/control-room/PromoteCutoverStrip'
import type { DeliveryPipelineRunView } from '@/api/deliveryTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'

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
  releasePending?: boolean
  tradeDeployPending?: boolean
  canDispatchRelease?: boolean
  canDispatchTradeDeploy?: boolean
  releaseDisabledReason?: string
  tradeDeployDisabledReason?: string
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
  onSatelliteLaunchAgentFix?: () => void
  satelliteLaunchAgentFixPending?: boolean
  satelliteLaunchAgentFixActive?: boolean
  satelliteLaunchAgentFixDisabled?: boolean
  satelliteLaunchAgentFixTitle?: string
}

type SummaryRowProps = Omit<OpsTaskStripsProps, 'promoteOnly'>

/** Mission Launch summary — see MissionLaunchBoard (tabbed Vehicle | Payload lanes). */
export function OpsTaskSummaryRow(props: SummaryRowProps) {
  const {
    mode,
    onNavigate,
    onDispatchRelease,
    onDispatchTradeDeploy,
    releasePending,
    tradeDeployPending,
    canDispatchRelease,
    canDispatchTradeDeploy,
    releaseDisabledReason,
    tradeDeployDisabledReason,
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

  const isMissionLaunch = mode.id === 'mission-launch'
  const showLaunchPad =
    ops.showLaunchPad &&
    isMissionLaunch &&
    (onDispatchRelease != null || onDispatchTradeDeploy != null)

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

  if (isMissionLaunch && showLaunchPad && (launchVerdict != null || satelliteLaunchVerdict != null)) {
    return (
      <MissionLaunchBoard
        onNavigate={onNavigate}
        launchVerdict={launchVerdict}
        launchCheckpoints={launchCheckpoints}
        satelliteLaunchVerdict={satelliteLaunchVerdict}
        satelliteLaunchCheckpoints={satelliteLaunchCheckpoints}
        onDispatchRelease={onDispatchRelease}
        onDispatchTradeDeploy={onDispatchTradeDeploy}
        releasePending={releasePending}
        tradeDeployPending={tradeDeployPending}
        canDispatchRelease={canDispatchRelease}
        canDispatchTradeDeploy={canDispatchTradeDeploy}
        releaseDisabledReason={releaseDisabledReason}
        tradeDeployDisabledReason={tradeDeployDisabledReason}
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
      />
    )
  }

  if (mode.id === 'daily-ops') {
    return <DailyOpsFleetDesk props={props} />
  }

  return null
}

function DailyOpsFleetDesk({
  props,
}: {
  props: SummaryRowProps
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
        {fleetWorkflow != null && (
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
        )}

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

        {/* Checklist | Fleet Board + in-column Cell Detail */}
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
                      onNavigate('agent-desk')
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
      </div>
    </DailyOpsProvider>
  )
}

export function OpsTaskStrips(props: OpsTaskStripsProps) {
  const {
    mode,
    context,
    matrices = [],
    stgSmoke,
    stgGate,
    lastDeliverSucceeded,
    tierB,
    onNavigate,
    onOpenPromote,
    onOpenDelivery,
    onDispatchRelease,
    onDispatchTradeDeploy,
    releasePending,
    tradeDeployPending,
    canDispatchRelease,
    canDispatchTradeDeploy,
    releaseDisabledReason,
    tradeDeployDisabledReason,
    promoteOnly = false,
  } = props
  const ops = mode.ops
  if (ops == null) return null

  /** Release posture (STG/Prod · Tier A·B) — Mission Launch TCC only; not Daily Ops. */
  const promoteSection =
    mode.id === 'mission-launch' ? (
      <Collapsible defaultOpen className="group/release rounded-lg border border-border bg-card px-3 py-1.5">
        <CollapsibleTrigger asChild>
          <button
            type="button"
            className="flex w-full cursor-pointer flex-wrap items-center gap-2 text-left"
          >
            <ChevronRight
              className="size-3.5 shrink-0 text-muted-foreground transition-transform group-data-[state=open]/release:rotate-90"
              aria-hidden
            />
            <span className="text-[var(--text-dense-meta)] font-semibold">Release posture</span>
            <DenseTag variant="neutral" className="text-[9px]">
              STG / Prod
            </DenseTag>
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              Promote / cutover · Tier A·B
            </span>
          </button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="mt-2">
            <PromoteCutoverStrip
              context={context}
              matrices={matrices}
              stgSmoke={stgSmoke}
              stgGate={stgGate}
              lastDeliverSucceeded={lastDeliverSucceeded}
              tierB={tierB}
              onOpenPromote={onOpenPromote}
              onOpenDelivery={onOpenDelivery}
            />
          </div>
        </CollapsibleContent>
      </Collapsible>
    ) : null

  if (promoteOnly) {
    return promoteSection != null ? <div className="flex flex-col gap-3">{promoteSection}</div> : null
  }

  const isPlaybookLaunch = mode.id === 'mission-launch'

  const launchPadSection =
    ops.showLaunchPad &&
    isPlaybookLaunch &&
    (onDispatchRelease != null || onDispatchTradeDeploy != null) ? (
      <OpsSection title="Mission launch">
        <LaunchPad
          variant={
            onDispatchRelease != null && onDispatchTradeDeploy != null
              ? 'both'
              : onDispatchRelease != null
                ? 'rocket-launch'
                : 'satellite-deploy'
          }
          onDispatchRelease={onDispatchRelease ?? (() => {})}
          onDispatchTradeDeploy={onDispatchTradeDeploy ?? (() => {})}
          releasePending={releasePending}
          tradeDeployPending={tradeDeployPending}
          canDispatchRelease={canDispatchRelease && onDispatchRelease != null}
          canDispatchTradeDeploy={canDispatchTradeDeploy && onDispatchTradeDeploy != null}
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
          onOpenPlatformRelease={() => onNavigate('platform-release')}
          onOpenTradeDeploy={() => onNavigate('trade-release')}
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
        <TaskModeReadinessStrip modeId="mission-launch" onNavigate={onNavigate} />
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

  if (mode.id === 'daily-ops') {
    // Fleet Desk is rendered via OpsTaskSummaryRow; Daily Ops never shows Release posture.
    return promoteOnly ? null : (
      <div className="flex flex-col gap-3">
        <DailyOpsFleetDesk props={props} />
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {readinessSection}
    </div>
  )
}
