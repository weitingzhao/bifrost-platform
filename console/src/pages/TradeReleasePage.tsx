import { DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useState } from 'react'
import {
  fetchDeliveryPipelines,
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchStgSmoke,
  fetchSupplyChain,
  fetchTierBStatus,
} from '@/api/platform'
import type { OpsContextResponse } from '@/api/types'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { DeliveryReleaseWorkflowPanel } from '@/components/delivery/DeliveryReleaseWorkflowPanel'
import { DeployActionBar } from '@/components/delivery/DeployActionBar'
import { GateActionBar } from '@/components/delivery/GateActionBar'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import {
  ReleaseStepCommandCenter,
  runStepStatus,
  gateStepStatus,
  type FlowStep,
} from '@/components/delivery/ReleaseStepCommandCenter'
import { ReleaseStateBanner } from '@/components/delivery/ReleaseStateBanner'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { StgTierBChecklistPanel } from '@/components/delivery/StgTierBChecklistPanel'
import { SupplyChainPanel } from '@/components/delivery/SupplyChainPanel'
import { TradeEnvAccessBar } from '@/components/delivery/TradeEnvAccessBar'
import { ReleaseGateCompareSection } from '@/components/promote/ReleaseGateCompareSection'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { isPipelineRunFailed, isPipelineRunRunning } from '@/lib/delivery/pipelineRunAskPack'

const TRADE_STG_TARGET = deliveryTargetById('trade-stg')
const TRADE_PROD_TARGET = deliveryTargetById('trade-prod')
const STG_PIPELINE = TRADE_STG_TARGET.pipeline
const PROD_PIPELINE = TRADE_PROD_TARGET.pipeline

const STEP_LABELS = ['Staging Deploy', 'Staging Gate', 'Production Deploy', 'Production Gate'] as const

function renderTradeStepActions(activeIndex: number) {
  switch (activeIndex) {
    case 0:
      return (
        <DeployActionBar
          target={TRADE_STG_TARGET}
          releaseStateTier="trade"
          deployButtonLabel={`Run ${STG_PIPELINE}`}
        />
      )
    case 1:
      return <GateActionBar tier="stg" label="STG" />
    case 2:
      return (
        <DeployActionBar
          target={TRADE_PROD_TARGET}
          releaseStateTier="trade"
          deployButtonLabel={`Run ${PROD_PIPELINE}`}
        />
      )
    default:
      return <GateActionBar tier="prod" label="PROD" />
  }
}

function SupplyChainSummaryLine() {
  const { data, isLoading } = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 15_000,
  })
  if (isLoading) return <span className="text-dense-meta text-muted-foreground">Supply chain…</span>
  const repos = data?.tracked_repos ?? []
  const cms = data?.dockerfile_configmaps?.filter(cm => cm.present).length ?? 0
  const cmTotal = data?.dockerfile_configmaps?.length ?? 0
  return (
    <span className="inline-flex flex-wrap items-center gap-2 text-dense-meta text-muted-foreground">
      <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
        Supply chain
      </span>
      <DenseTag variant={repos.length >= 7 ? 'success' : 'warning'}>
        {repos.length} Gitea repos tracked
      </DenseTag>
      <DenseTag variant={cms === cmTotal ? 'success' : 'warning'}>
        Dockerfile CMs {cms}/{cmTotal}
      </DenseTag>
    </span>
  )
}

interface TradeReleasePageProps {
  context: OpsContextResponse | undefined
  isLoading?: boolean
  onOpenPlacement?: () => void
}

export function TradeReleasePage({ context, isLoading = false, onOpenPlacement }: TradeReleasePageProps) {
  const [activeIndex, setActiveIndex] = useState(0)

  const stgRuns = useQuery({
    queryKey: ['delivery', 'runs', STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(STG_PIPELINE),
    refetchInterval: 15_000,
  })
  const prodRuns = useQuery({
    queryKey: ['delivery', 'runs', PROD_PIPELINE],
    queryFn: () => fetchPipelineRuns(PROD_PIPELINE),
    refetchInterval: 15_000,
  })
  const stgGate = useQuery({
    queryKey: ['promote', 'release-gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 30_000,
  })
  const prodGate = useQuery({
    queryKey: ['promote', 'release-gate', 'prod'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: 30_000,
  })
  const stgSmoke = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
  })
  const tierB = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: 30_000,
  })
  const pipelines = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })

  if (isLoading || !context) {
    return <p className="text-muted-foreground">Loading release context…</p>
  }

  const stgDeploy = runStepStatus(stgRuns.data?.runs?.[0])
  const prodDeploy = runStepStatus(prodRuns.data?.runs?.[0])
  const stgGateStep = gateStepStatus(stgGate.data)
  const prodGateStep = gateStepStatus(prodGate.data)

  const steps: FlowStep[] = [
    { key: 'stg-deploy', label: 'Staging Deploy', env: 'STG', status: stgDeploy.status, statusLabel: stgDeploy.label },
    { key: 'stg-gate', label: 'Staging Gate', env: 'STG', status: stgGateStep.status, statusLabel: stgGateStep.label },
    { key: 'prod-deploy', label: 'Production Deploy', env: 'PROD', status: prodDeploy.status, statusLabel: prodDeploy.label },
    { key: 'prod-gate', label: 'Production Gate', env: 'PROD', status: prodGateStep.status, statusLabel: prodGateStep.label },
  ]

  const lastStgRun = stgRuns.data?.runs?.[0]
  const lastProdRun = prodRuns.data?.runs?.[0]
  const showStgActiveRun =
    activeIndex === 0
    && lastStgRun != null
    && (isPipelineRunRunning(lastStgRun) || isPipelineRunFailed(lastStgRun))
  const showProdActiveRun =
    activeIndex === 2
    && lastProdRun != null
    && (isPipelineRunRunning(lastProdRun) || isPipelineRunFailed(lastProdRun))

  const renderStepDetail = (index: number): ReactNode => {
    switch (index) {
      case 0:
        return (
          <div className="mt-3 flex flex-col gap-3 border-t border-border/40 pt-3">
            <SupplyChainPanel layout="operate" />
            {showStgActiveRun && <DeliveryActiveRunPanel target={TRADE_STG_TARGET} />}
          </div>
        )
      case 1:
        return (
          <div className="mt-3 flex flex-col gap-3 border-t border-border/40 pt-3">
            <StgSmokePanel
              data={stgSmoke.data}
              isLoading={stgSmoke.isLoading}
              isFetching={stgSmoke.isFetching}
              errorMessage={stgSmoke.error instanceof Error ? stgSmoke.error.message : null}
              onRefresh={() => void stgSmoke.refetch()}
              title="STG HTTP smoke"
              description="Post-deliver acceptance via trade-stg gateway."
            />
            <StgTierBChecklistPanel
              tierB={tierB.data}
              tierBLoading={tierB.isLoading}
              layout="observe"
            />
          </div>
        )
      case 2:
        return (
          <div className="mt-3 flex flex-col gap-3 border-t border-border/40 pt-3">
            <PlatformDeliverActuatePanel target={TRADE_PROD_TARGET} hideActions />
            {showProdActiveRun && <DeliveryActiveRunPanel target={TRADE_PROD_TARGET} />}
            <PipelineRunsPanel
              pipelines={pipelines.data}
              pipelinesLoading={pipelines.isLoading}
              errorMessage={pipelines.error instanceof Error ? pipelines.error.message : null}
              layout="observe"
              onOpenPlacement={onOpenPlacement}
            />
          </div>
        )
      case 3:
        return (
          <div className="mt-3 border-t border-border/40 pt-3">
            <ReleaseGateCompareSection
              stgGate={stgGate.data}
              stgGateLoading={stgGate.isLoading}
              stgGateError={stgGate.error instanceof Error ? stgGate.error.message : null}
              prodGate={prodGate.data}
              prodGateLoading={prodGate.isLoading}
              prodGateError={prodGate.error instanceof Error ? prodGate.error.message : null}
            />
          </div>
        )
      default:
        return null
    }
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-2 rounded-lg border border-border/50 bg-secondary/30 px-4 py-2.5">
        <TradeEnvAccessBar />
        <SupplyChainSummaryLine />
        <ReleaseStateBanner tier="trade" />
      </div>

      <ReleaseStepCommandCenter
        steps={steps}
        activeIndex={activeIndex}
        onSelect={setActiveIndex}
        stepLabels={STEP_LABELS}
        stgRun={stgRuns.data?.runs?.[0]}
        prodRun={prodRuns.data?.runs?.[0]}
        stgGate={stgGate.data}
        prodGate={prodGate.data}
        renderStepActions={renderTradeStepActions}
        renderStepDetail={renderStepDetail}
      />

      <details className="group rounded-lg border border-border/50 bg-card">
        <summary className="cursor-pointer list-none px-4 py-3 text-dense-label font-medium text-foreground hover:bg-secondary/30">
          CI/CD pipeline topology and release workflow
        </summary>
        <div className="flex flex-col gap-4 border-t border-border/50 px-4 py-3">
          <DeliveryReleaseWorkflowPanel context={context} stgSmoke={stgSmoke.data} />
          <DeliveryFlow context={context} />
        </div>
      </details>
    </div>
  )
}
