import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { DenseTag } from '@bifrost/ui'
import { Bot, Rocket, Satellite } from 'lucide-react'
import {
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSupplyChain,
  fetchStgSmoke,
} from '@/api/platform'
import { LaunchPad } from '@/components/control-room/LaunchPad'
import { gateStepStatus, runStepStatus, pickDeployPipelineRun, deployRunRetryFailed } from '@/components/delivery/ReleaseStepCommandCenter'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  DailyOpsMissionStrip,
  TaskModeReadinessStrip,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { LaunchLiveView } from '@/components/task-mode/LaunchLiveView'
import { MissionLaunchBoard } from '@/components/task-mode/MissionLaunchBoard'
import { useOperateQueue } from '@/hooks/useOperateQueue'
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
  onOpenAgentDesk?: () => void
  /** Ambient agent job — opens Launch Live View for trade-deploy / release scope. */
  ambientJobId?: string | null
  ambientJobScope?: string | null
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

function OperateQueueSummary({ onNavigate }: { onNavigate: (tab: string) => void }) {
  const queueQ = useOperateQueue()
  const open = queueQ.data?.open ?? []

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex items-center gap-2">
        <Bot size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Operate queue</span>
        <DenseTag variant={open.length === 0 ? 'success' : 'warning'}>
          {open.length === 0 ? 'Clear' : `${open.length} open`}
        </DenseTag>
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
        Review in Control Room →
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
    return (
      <div className="grid gap-3 md:grid-cols-2">
        <OpsSection title="Operate summary" bodyPadding="compact">
          <OperateQueueSummary onNavigate={onNavigate} />
        </OpsSection>
        {ops.showMissionSignals && (
          <OpsSection title="Live signals" bodyPadding="compact">
            <DailyOpsMissionStrip compact />
          </OpsSection>
        )}
      </div>
    )
  }

  return null
}

export function OpsTaskStrips({
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
}: OpsTaskStripsProps) {
  const ops = mode.ops
  if (ops == null) return null

  const promoteSection =
    mode.id === 'daily-ops' ? (
      <OpsSection title="Promote / cutover">
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
      </OpsSection>
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
    ) : mode.id === 'daily-ops' && ops.showMissionSignals ? (
      <OpsSection title="Live signals">
        <DailyOpsMissionStrip />
      </OpsSection>
    ) : null

  const operateSummarySection =
    mode.id === 'daily-ops' ? (
      <OpsSection title="Operate summary">
        <OperateQueueSummary onNavigate={onNavigate} />
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
        {operateSummarySection}
        {readinessSection}
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
