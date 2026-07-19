import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DenseTag } from '@bifrost/ui'
import { Bot, Loader2, Rocket, Satellite } from 'lucide-react'
import {
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSupplyChain,
  fetchStgSmoke,
} from '@/api/platform'
import { LaunchPad } from '@/components/control-room/LaunchPad'
import { gateStepStatus, runStepStatus, pickDeployPipelineRun, deployRunRetryFailed } from '@/components/delivery/ReleaseStepCommandCenter'
import { OpsSection } from '@/components/layout/OpsSection'
import { DailyOpsFleetBoard } from '@/components/task-mode/DailyOpsFleetBoard'
import { DailyOpsFleetCellDetail } from '@/components/task-mode/DailyOpsFleetCellDetail'
import {
  DailyOpsAgentLivePanel,
  DailyOpsProcessStrip,
} from '@/components/task-mode/DailyOpsProcessStrip'
import { DailyOpsOperatorPlanPanel } from '@/components/task-mode/DailyOpsOperatorPlanPanel'
import { DAILY_OPS_CHECKLIST_RUN_SCOPE } from '@/lib/agent/agentScopes'
import type { DailyOpsWorkflowResult } from '@/lib/control-room/dailyOpsWorkflow'
import { TaskModeReadinessStrip } from '@/components/task-mode/TaskModeReadinessStrip'
import { LaunchLiveView } from '@/components/task-mode/LaunchLiveView'
import { MissionLaunchBoard } from '@/components/task-mode/MissionLaunchBoard'
import { useFleetSnapshot } from '@/hooks/useFleetSnapshot'
import { useDailyOpsChecklistCoverage } from '@/hooks/useDailyOpsChecklistCoverage'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { operateQueueClearLabel, type FleetCell } from '@/lib/control-room/fleetSnapshot'
import { buildStgReleasePhases } from '@/lib/architecture/deliveryMainlineCatalog'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import { deliveryFocusRunQueryKey } from '@/lib/delivery/deliveryFocusRun'
import { TRADE_DEPLOY_SCOPE } from '@/lib/agent/tradeDeployAgentPrompt'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { PromoteCutoverStrip } from '@/components/control-room/PromoteCutoverStrip'
import type { DeliveryPipelineRunView, MatrixResponse, OpsContextResponse } from '@/api/types'
import type { TaskModeDef } from '@/lib/task-mode/types'
import type { LaunchCheckpoint, LaunchVerdict } from '@/lib/task-mode/satelliteLaunchVerdict'

export type OpsTaskStripsProps = {
  mode: TaskModeDef
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  stgSmoke?: import('@/api/types').StgSmokeResponse
  stgGate?: import('@/api/types').ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: import('@/api/types').TierBStatusResponse
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
  /** When true, only render Promote / cutover (summary row rendered separately). */
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
  /** Single primary CTA path for Daily Ops Process strip (replaces dual Verdict+Workflow). */
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
  recentRuns?: import('@/api/types').DeliveryPipelineRunView[]
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
  onOpenAgentDesk?: (jobId?: string) => void
  /** Ambient agent job — opens Launch Live View for trade-deploy / release scope. */
  ambientJobId?: string | null
  ambientJobScope?: string | null
  /** Checklist auto-dispatch / related remediation jobs for Action column. */
  activeDispatchJobs?: import('@/api/types').RemediationJob[]
  /** Namespace for the mode's deliver pipeline runs (trade STG or platform). */
  pipelineRunsNamespace?: string
  /** Rocket Launch Live View post-deploy chips. */
  platformStgGate?: import('@/api/types').ReleaseGateResponse
  platformProdGate?: import('@/api/types').ReleaseGateResponse
  supplyCmsPresent?: number
  supplyCmsTotal?: number
  /** Mission Launch — trade pipeline alongside platform recentRuns. */
  tradeRecentRuns?: import('@/api/types').DeliveryPipelineRunView[]
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

function OperateQueueSummary({
  onNavigate,
  fleetClear,
}: {
  onNavigate: (tab: string) => void
  fleetClear: boolean
}) {
  const queueQ = useOperateQueue()
  const open = queueQ.data?.open ?? []
  const label = operateQueueClearLabel(open.length, fleetClear)
  const clearButFleetNot = open.length === 0 && !fleetClear

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Bot size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Operate queue</span>
        <DenseTag
          variant={open.length === 0 && fleetClear ? 'success' : clearButFleetNot ? 'neutral' : 'warning'}
        >
          {label}
        </DenseTag>
        {clearButFleetNot && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            Queue clear does not mean fleet clear
          </span>
        )}
      </div>
      {open.length > 0 && (
        <ul className="m-0 mt-2 list-none space-y-1 p-0">
          {open.slice(0, 3).map(item => (
            <li key={item.id} className="text-[var(--text-dense-meta)] text-muted-foreground">
              {item.title}
            </li>
          ))}
        </ul>
      )}
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('control-room')}
      >
        Review operate items →
      </button>
    </div>
  )
}

export function PlatformStgReleaseStrip({
  onNavigate,
  compact = false,
}: {
  onNavigate: (tab: string) => void
  compact?: boolean
}) {
  const platformRunsQ = useQuery({
    queryKey: ['task-cc', 'platform-runs-summary'],
    queryFn: () => fetchPipelineRuns(DELIVER_PLATFORM_PIPELINE),
    refetchInterval: 20_000,
  })
  const platformStgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate-summary'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
  })
  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply-chain-summary'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
  })

  const gate = gateStepStatus(platformStgGateQ.data)
  const runs = platformRunsQ.data?.runs
  const run = pickDeployPipelineRun(runs, {
    gatePassed: platformStgGateQ.data?.result === 'pass',
  })
  const deploy = runStepStatus(run)
  const retryFailed = deployRunRetryFailed(runs, run)
  const cms = supplyQ.data?.dockerfile_configmaps ?? []
  const cmsPresent = cms.filter(c => c.present).length

  if (compact) {
    return (
      <div className="flex flex-col gap-1 border-t border-border/50 pt-1.5">
        <span className="text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground">
          Last STG deliver
        </span>
        <div className="flex flex-wrap gap-1">
          <DenseTag variant="neutral" className="text-[8px]">
            {deploy.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            Gate {gate.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            CM {cmsPresent}/{cms.length}
          </DenseTag>
          {retryFailed && (
            <DenseTag variant="neutral" className="text-[8px]">
              retry fail
            </DenseTag>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Rocket size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Platform STG mainline</span>
        <DenseTag variant="neutral" className="text-[9px]">
          Last run
        </DenseTag>
        <DenseTag variant={cmsPresent === cms.length && cms.length > 0 ? 'success' : 'warning'}>
          CMs {cmsPresent}/{cms.length}
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
        {run?.revision != null ? `Revision ${run.revision}` : 'bifrost-deliver-platform pipeline'}
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        <DenseTag variant={deploy.status === 'done' ? 'success' : deploy.status === 'error' ? 'warning' : 'warning'}>
          Deploy · {deploy.label}
        </DenseTag>
        {retryFailed && (
          <DenseTag variant="neutral" className="text-[9px]">
            Latest retry failed
          </DenseTag>
        )}
        <DenseTag variant={gate.status === 'done' ? 'success' : 'warning'}>Gate · {gate.label}</DenseTag>
      </div>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('platform-release')}
      >
        Launch Rocket →
      </button>
    </div>
  )
}

function SupplyChainStrip({ onNavigate }: { onNavigate: (tab: string) => void }) {
  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
  })
  const cms = supplyQ.data?.dockerfile_configmaps ?? []
  const present = cms.filter(c => c.present).length

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex items-center gap-2">
        <Rocket size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Platform supply chain</span>
        <DenseTag variant={present === cms.length && cms.length > 0 ? 'success' : 'warning'}>
          CMs {present}/{cms.length}
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-muted-foreground">
        Mirrors {supplyQ.data?.mirror_credentials_configured ? 'configured' : 'check credentials'}
      </p>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('platform-release')}
      >
        Launch Rocket →
      </button>
    </div>
  )
}

function StgReleaseStrip({
  context,
  onNavigate,
  compact = false,
}: {
  context?: OpsContextResponse
  onNavigate: (tab: string) => void
  compact?: boolean
}) {
  const phases = buildStgReleasePhases(context)
  const active = phases.find(p => p.status === 'active') ?? phases.find(p => p.status === 'blocked')
  const done = phases.filter(p => p.status === 'done').length

  const tradeRunsQ = useQuery({
    queryKey: ['task-cc', 'trade-runs'],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    refetchInterval: 20_000,
  })
  const tradeGateQ = useQuery({
    queryKey: ['task-cc', 'trade-gate'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
  })
  const smokeQ = useQuery({
    queryKey: ['task-cc', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 20_000,
  })

  const gate = gateStepStatus(tradeGateQ.data)
  const smokeOk = smokeQ.data?.reachability === 'ok'
  const runs = tradeRunsQ.data?.runs
  const run = pickDeployPipelineRun(runs, {
    gatePassed: tradeGateQ.data?.result === 'pass',
    smokeOk,
  })
  const deploy = runStepStatus(run)
  const retryFailed = deployRunRetryFailed(runs, run)

  if (compact) {
    return (
      <div className="flex flex-col gap-1 border-t border-border/50 pt-1.5">
        <span className="text-[var(--text-dense-micro)] font-medium uppercase tracking-wide text-muted-foreground">
          Last STG deliver
        </span>
        <div className="flex flex-wrap gap-1">
          <DenseTag variant="neutral" className="text-[8px]">
            {deploy.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            Gate {gate.label}
          </DenseTag>
          <DenseTag variant="neutral" className="text-[8px]">
            Smoke {smokeOk ? 'ok' : smokeQ.isLoading ? '…' : 'fail'}
          </DenseTag>
          {retryFailed && (
            <DenseTag variant="neutral" className="text-[8px]">
              retry fail
            </DenseTag>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Satellite size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Trade STG deliver</span>
        <DenseTag variant="neutral" className="text-[9px]">
          Last run
        </DenseTag>
        <DenseTag variant="neutral">
          {done}/{phases.length} phases
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
        bifrost-deliver-stg · smoke + gate (pre-prod checkpoint)
      </p>
      <p className="m-0 mt-0.5 text-[var(--text-dense-meta)]">
        {active != null ? `${active.title} · ${active.status}` : 'All phases complete or planned'}
      </p>
      <div className="mt-2 flex flex-wrap gap-2">
        <DenseTag variant={deploy.status === 'done' ? 'success' : deploy.status === 'error' ? 'warning' : 'warning'}>
          Deploy · {deploy.label}
        </DenseTag>
        {retryFailed && (
          <DenseTag variant="neutral" className="text-[9px]">
            Latest retry failed
          </DenseTag>
        )}
        <DenseTag variant={gate.status === 'done' ? 'success' : 'warning'}>Gate · {gate.label}</DenseTag>
        <DenseTag variant={smokeOk ? 'success' : 'warning'}>
          Smoke · {smokeOk ? 'pass' : smokeQ.isLoading ? '…' : 'fail'}
        </DenseTag>
      </div>
      <button
        type="button"
        className="mt-2 text-[var(--text-dense-meta)] text-primary hover:underline"
        onClick={() => onNavigate('trade-release')}
      >
        Deploy Satellite →
      </button>
    </div>
  )
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
        onOpenAgentDesk={
          onOpenAgentDesk != null ? () => onOpenAgentDesk() : undefined
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
        onOpenAgentDesk={
          onOpenAgentDesk != null ? () => onOpenAgentDesk() : undefined
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
    return <DailyOpsFleetDesk props={props} compact />
  }

  return null
}

function DailyOpsFleetDesk({
  props,
  compact = false,
}: {
  props: SummaryRowProps
  compact?: boolean
}) {
  const { fleet, isLoading } = useFleetSnapshot()
  const checklistCoverage = useDailyOpsChecklistCoverage(fleet)
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
    ambientJobId,
    ambientJobScope,
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

  return (
    <div className="flex min-w-0 max-w-full flex-col gap-3">
      {/* Agent progress pinned above process strip while remediating */}
      {hasAmbientJob && ambientJobId != null && (
        <div className="sticky top-0 z-20 -mx-0.5 bg-[var(--card)]/95 px-0.5 py-0.5 backdrop-blur-sm">
          <DailyOpsAgentLivePanel
            jobId={ambientJobId}
            jobScope={ambientJobScope}
            onOpenAgentDesk={onOpenAgentDesk}
          />
        </div>
      )}
      {showStartingHint && (
        <div className="flex items-center gap-1.5 rounded-md border border-border/60 bg-secondary px-3 py-2 text-[var(--text-dense-caption)] text-muted-foreground">
          <Loader2 className="size-3 animate-spin" aria-hidden />
          Starting Agent…
        </div>
      )}

      {fleetWorkflow != null && (
        <DailyOpsProcessStrip
          fleet={fleet}
          workflow={fleetWorkflow}
          isLoading={isLoading}
          canOperate={readinessCanOperate}
          agentFixPending={fleetAgentFixPending}
          agentFixError={stripError}
          showReadyHint
          ambientJobId={ambientJobId}
          onPrimaryAction={() => {
            if (fleetWorkflow.primaryAction.kind === 'operator-plan') {
              onOperatorPlanFix?.()
              return
            }
            onFleetWorkflowAction?.()
          }}
          onOpenAgentDesk={onOpenAgentDesk}
          onOpenFullOperatorPlane={() => onNavigate('operator-plane')}
          operatorPlanFixPending={operatorPlanFixPending}
          operatorPlanFixDisabled={operatorPlanFixDisabled}
          operatorPlanFixTitle={operatorPlanFixTitle}
          checklistCheckPending={checklistCheckPending}
          checklistCheckDisabled={checklistCheckDisabled}
          checklistCheckTitle={checklistCheckTitle}
          checklistCheckActive={checklistCheckActive || isChecklistAmbient}
          checklistCheckStatusHint={checklistCheckStatusHint}
        />
      )}

      {/* Checklist | Fleet Board — side-by-side from xl */}
      <div className="grid min-w-0 grid-cols-1 gap-3 xl:grid-cols-[minmax(0,1.2fr)_minmax(0,0.9fr)] xl:items-start">
        {!isLoading && (
          <div className="min-w-0 rounded-lg border border-border bg-secondary px-3 py-2">
            <DailyOpsOperatorPlanPanel
              engineerCell={engineerCell}
              fleet={fleet}
              coverage={checklistCoverage}
              activeFlashStepId={activeFlashStepId}
              onFlashStep={handleFlashChecklistStep}
              onOpenFullOperatorPlane={() => onNavigate('operator-plane')}
              activeDispatchJobs={activeDispatchJobs}
              onChecklistCheck={onChecklistCheck}
              checklistCheckPending={checklistCheckPending}
              checklistCheckDisabled={checklistCheckDisabled}
              checklistCheckTitle={checklistCheckTitle}
              checklistCheckError={checklistCheckError}
              checklistCheckActive={checklistCheckActive || isChecklistAmbient}
              checklistCheckStatusHint={checklistCheckStatusHint}
              onChecklistItemFix={onChecklistItemFix}
              checklistItemFixPending={checklistItemFixPending}
              checklistItemFixDisabled={checklistItemFixDisabled}
              checklistItemFixTitle={checklistItemFixTitle}
              checklistItemFixError={checklistItemFixError}
              checklistItemFixActiveId={checklistItemFixActiveId}
              ambientJobId={ambientJobId}
              ambientJobScope={ambientJobScope}
              onOpenDispatchJob={jobId => onOpenAgentDesk?.(jobId)}
              onOpenOperateQueue={
                onOpenOperateQueue ?? (() => onNavigate('control-room'))
              }
              compactColumns
            />
          </div>
        )}
        <div className="flex min-w-0 flex-col gap-3">
          <DailyOpsFleetBoard
            fleet={fleet}
            isLoading={isLoading}
            canOperate={readinessCanOperate}
            agentFixPending={fleetAgentFixPending}
            selectedCellKey={selectedCellKey}
            coverage={checklistCoverage}
            flashKeys={flashKeys}
            flashNonce={flashNonce}
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
              onAgentFix={onFleetCellFix}
              onNavigate={onNavigate}
              onClose={() => setSelectedCellKey(null)}
            />
          )}
        </div>
      </div>

      <div className={compact ? 'min-w-0' : 'grid min-w-0 gap-3 md:grid-cols-2'}>
        <OpsSection title="Operate summary" bodyPadding="compact">
          <OperateQueueSummary onNavigate={onNavigate} fleetClear={fleet.fleetClear} />
        </OpsSection>
      </div>
    </div>
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

  const promoteSection =
    mode.id === 'daily-ops' ? (
      <details className="rounded-lg border border-border bg-card px-3 py-1.5">
        <summary className="cursor-pointer list-none [&::-webkit-details-marker]:hidden">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-meta)] font-semibold">Release posture</span>
            <DenseTag variant="neutral" className="text-[9px]">
              Secondary
            </DenseTag>
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              Promote / cutover — not first-screen Daily Ops
            </span>
          </div>
        </summary>
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
      </details>
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
          variant="both"
          onDispatchRelease={onDispatchRelease ?? (() => {})}
          onDispatchTradeDeploy={onDispatchTradeDeploy ?? (() => {})}
          releasePending={releasePending}
          tradeDeployPending={tradeDeployPending}
          canDispatchRelease={canDispatchRelease}
          canDispatchTradeDeploy={canDispatchTradeDeploy}
          releaseDisabledReason={releaseDisabledReason}
          tradeDeployDisabledReason={tradeDeployDisabledReason}
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
    return (
      <div className="flex flex-col gap-3">
        {!promoteOnly && <DailyOpsFleetDesk props={props} />}
        {promoteSection}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {promoteSection}
      {readinessSection}
    </div>
  )
}
