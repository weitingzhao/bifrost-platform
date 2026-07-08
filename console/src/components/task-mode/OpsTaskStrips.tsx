import { useQuery } from '@tanstack/react-query'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import { Bot, Gauge, Rocket, Satellite } from 'lucide-react'
import {
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSupplyChain,
  fetchStgSmoke,
} from '@/api/platform'
import { LaunchPad } from '@/components/control-room/LaunchPad'
import { gateStepStatus, runStepStatus } from '@/components/delivery/ReleaseStepCommandCenter'
import { OpsSection } from '@/components/layout/OpsSection'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { buildStgReleasePhases } from '@/lib/architecture/deliveryMainlineCatalog'
import { missionStatus, missionStatusColor } from '@/lib/control-room/missionSignals'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
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
}

function MissionSignalStrip() {
  const { snapshot, isLoading } = useMissionSnapshot()
  const status = missionStatus(snapshot.missionOverall)
  const color = missionStatusColor(status)

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <Gauge size={16} style={{ color }} />
        <span className="text-[var(--text-dense-label)] font-semibold">Mission signals</span>
        <StatusLamp value={snapshot.missionOverall} kind="reach" />
        <DenseTag variant={status === 'NOMINAL' ? 'success' : status === 'CRITICAL' ? 'danger' : 'warning'}>
          {isLoading ? 'Probing…' : status}
        </DenseTag>
      </div>
      <div className="mt-2 grid gap-2 sm:grid-cols-3">
        <SignalChip label="Rocket" signal={snapshot.rocketOverall} detail={snapshot.release.detail} />
        <SignalChip label="Payload" signal={snapshot.payloadOverall} detail={snapshot.tradeProd.detail} />
        <SignalChip label="Infra" signal={snapshot.infra.signal} detail={snapshot.infra.detail} />
      </div>
    </div>
  )
}

function SignalChip({
  label,
  signal,
  detail,
}: {
  label: string
  signal: import('@/lib/control-room/missionSignals').Signal
  detail: string
}) {
  return (
    <div className="rounded border border-border/60 bg-card px-2 py-1.5">
      <div className="flex items-center gap-1.5">
        <StatusLamp value={signal} kind="reach" />
        <span className="text-[var(--text-dense-meta)] font-medium">{label}</span>
      </div>
      <p className="m-0 mt-0.5 truncate text-[var(--text-dense-caption)] text-muted-foreground">{detail}</p>
    </div>
  )
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

  const run = tradeRunsQ.data?.runs?.[0]
  const deploy = runStepStatus(run)
  const gate = gateStepStatus(tradeGateQ.data)

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
        <DenseTag variant={deploy.status === 'done' ? 'success' : 'warning'}>Deploy · {deploy.label}</DenseTag>
        <DenseTag variant={gate.status === 'done' ? 'success' : 'warning'}>Gate · {gate.label}</DenseTag>
        <DenseTag variant={smokeQ.data?.reachability === 'ok' ? 'success' : 'warning'}>
          Smoke · {smokeQ.data?.reachability === 'ok' ? 'pass' : '…'}
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
}: OpsTaskStripsProps) {
  const ops = mode.ops
  if (ops == null) return null

  return (
    <div className="flex flex-col gap-3">
      {(mode.id === 'rocket-launch' || mode.id === 'satellite-deploy' || mode.id === 'daily-ops') && (
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
      )}

      {ops.showMissionSignals && (
        <OpsSection title="Live signals">
          <MissionSignalStrip />
        </OpsSection>
      )}

      {mode.id === 'daily-ops' && (
        <OpsSection title="Operate summary">
          <OperateQueueSummary onNavigate={onNavigate} />
        </OpsSection>
      )}

      {mode.id === 'rocket-launch' && ops.signalSource === 'supply-chain' && (
        <OpsSection title="Supply chain">
          <SupplyChainStrip onNavigate={onNavigate} />
        </OpsSection>
      )}

      {mode.id === 'satellite-deploy' && (
        <OpsSection title="STG release">
          <StgReleaseStrip context={context} onNavigate={onNavigate} />
        </OpsSection>
      )}

      {ops.showLaunchPad && onDispatchRelease != null && onDispatchTradeDeploy != null && (
        <OpsSection title="Launch pad">
          <LaunchPad
            onDispatchRelease={onDispatchRelease}
            onDispatchTradeDeploy={onDispatchTradeDeploy}
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
      )}
    </div>
  )
}
