import { useQuery } from '@tanstack/react-query'
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
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { buildStgReleasePhases } from '@/lib/architecture/deliveryMainlineCatalog'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import { PromoteCutoverStrip } from '@/components/control-room/PromoteCutoverStrip'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import type { TaskModeDef } from '@/lib/task-mode/types'

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

function PlatformStgReleaseStrip({ onNavigate }: { onNavigate: (tab: string) => void }) {
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

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Rocket size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">Platform STG mainline</span>
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
        Platform Release →
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
        Platform Release →
      </button>
    </div>
  )
}

function StgReleaseStrip({
  context,
  onNavigate,
}: {
  context?: OpsContextResponse
  onNavigate: (tab: string) => void
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

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Satellite size={16} />
        <span className="text-[var(--text-dense-label)] font-semibold">STG release mainline</span>
        <DenseTag variant="neutral">
          {done}/{phases.length} phases
        </DenseTag>
      </div>
      <p className="m-0 mt-1 text-[var(--text-dense-meta)]">
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
        Trade Deploy →
      </button>
    </div>
  )
}

type SummaryRowProps = Omit<OpsTaskStripsProps, 'promoteOnly'>

/** Three-column summary — launch + playbook context + live signals, above phase stepper. */
export function OpsTaskSummaryRow(props: SummaryRowProps) {
  const {
    mode,
    context,
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
  } = props
  const ops = mode.ops
  if (ops == null) return null

  const showLaunchPad =
    ops.showLaunchPad &&
    ((mode.id === 'rocket-launch' && onDispatchRelease != null) ||
      (mode.id === 'satellite-deploy' && onDispatchTradeDeploy != null))

  if (mode.id === 'rocket-launch' && showLaunchPad) {
    return (
      <div className="grid gap-3 lg:grid-cols-3">
        <OpsSection title="Rocket launch" bodyPadding="compact">
          <LaunchPad
            variant="rocket-launch"
            embedded
            suppressProdBlockedFeedback
            onDispatchRelease={onDispatchRelease!}
            onDispatchTradeDeploy={() => {}}
            releasePending={releasePending}
            canDispatchRelease={canDispatchRelease}
            releaseDisabledReason={releaseDisabledReason}
            onOpenPlatformRelease={() => onNavigate('platform-release')}
            onOpenTradeDeploy={() => onNavigate('trade-release')}
          />
        </OpsSection>
        <OpsSection title="Platform STG release" bodyPadding="compact">
          <PlatformStgReleaseStrip onNavigate={onNavigate} />
        </OpsSection>
        {ops.showMissionSignals && (
          <OpsSection title="Environment readiness" bodyPadding="compact">
            <TaskModeReadinessStrip
              modeId="rocket-launch"
              onNavigate={onNavigate}
              compact
              summaryColumn
              suppressProdBlockedBanner
            />
          </OpsSection>
        )}
      </div>
    )
  }

  if (mode.id === 'satellite-deploy' && showLaunchPad) {
    return (
      <div className="grid gap-3 lg:grid-cols-3">
        <OpsSection title="Satellite deploy" bodyPadding="compact">
          <LaunchPad
            variant="satellite-deploy"
            embedded
            suppressProdBlockedFeedback
            onDispatchRelease={() => {}}
            onDispatchTradeDeploy={onDispatchTradeDeploy!}
            tradeDeployPending={tradeDeployPending}
            canDispatchTradeDeploy={canDispatchTradeDeploy}
            tradeDeployDisabledReason={tradeDeployDisabledReason}
            onOpenPlatformRelease={() => onNavigate('platform-release')}
            onOpenTradeDeploy={() => onNavigate('trade-release')}
          />
        </OpsSection>
        <OpsSection title="STG release" bodyPadding="compact">
          <StgReleaseStrip context={context} onNavigate={onNavigate} />
        </OpsSection>
        {ops.showMissionSignals && (
          <OpsSection title="Environment readiness" bodyPadding="compact">
            <TaskModeReadinessStrip
              modeId="satellite-deploy"
              onNavigate={onNavigate}
              compact
              summaryColumn
              suppressProdBlockedBanner
              canOperate={readinessCanOperate}
              onAgentFixStg={onAgentFixStg}
              onAgentFixProd={onAgentFixProd}
              agentFixPending={agentFixPending}
              agentFixDisabled={agentFixDisabled}
              agentFixTitle={agentFixTitle}
              onAgentTriage={onAgentTriage}
              agentTriagePending={agentTriagePending}
              agentTriageDisabled={agentTriageDisabled}
              agentTriageTitle={agentTriageTitle}
            />
          </OpsSection>
        )}
      </div>
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
    mode.id === 'rocket-launch' || mode.id === 'satellite-deploy' || mode.id === 'daily-ops' ? (
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

  const isPlaybookLaunch = mode.id === 'rocket-launch' || mode.id === 'satellite-deploy'

  const launchPadSection =
    ops.showLaunchPad &&
    ((mode.id === 'rocket-launch' && onDispatchRelease != null) ||
      (mode.id === 'satellite-deploy' && onDispatchTradeDeploy != null)) ? (
      <OpsSection title={mode.id === 'rocket-launch' ? 'Rocket launch' : 'Satellite deploy'}>
        <LaunchPad
          variant={mode.id === 'rocket-launch' ? 'rocket-launch' : 'satellite-deploy'}
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
    mode.id === 'rocket-launch' && ops.signalSource === 'supply-chain' ? (
      <OpsSection title="Supply chain">
        <SupplyChainStrip onNavigate={onNavigate} />
      </OpsSection>
    ) : null

  const stgReleaseSection =
    mode.id === 'satellite-deploy' ? (
      <OpsSection title="STG release">
        <StgReleaseStrip context={context} onNavigate={onNavigate} />
      </OpsSection>
    ) : null

  const readinessSection =
    mode.id === 'rocket-launch' && ops.showMissionSignals ? (
      <OpsSection title="Environment readiness">
        <TaskModeReadinessStrip modeId="rocket-launch" onNavigate={onNavigate} />
      </OpsSection>
    ) : mode.id === 'satellite-deploy' && ops.showMissionSignals ? (
      <OpsSection title="Environment readiness">
        <TaskModeReadinessStrip modeId="satellite-deploy" onNavigate={onNavigate} />
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
        {mode.id === 'rocket-launch' ? supplyChainSection : stgReleaseSection}
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
