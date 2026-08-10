import type { SupplyChainResponse } from '@/api/deliveryTypes'
import { Button, DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { fetchDeliveryPipelines, fetchPipelineRuns, fetchSupplyChain } from '@/api/delivery'
import { fetchGitOpsApps } from '@/api/gitOps'
import { fetchReleaseGate, fetchStgSmoke, fetchTierBStatus } from '@/api/promote'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { GitOpsQuickActionsPanel } from '@/components/delivery/GitOpsQuickActionsPanel'
import { DeliveryReleaseWorkflowPanel } from '@/components/delivery/DeliveryReleaseWorkflowPanel'
import { DeployActionBar } from '@/components/delivery/DeployActionBar'
import { GateActionBar } from '@/components/delivery/GateActionBar'
import {
  LaneDetailCollapse,
  LaneDetailContextStrip,
  LaneGateSummaryLine,
  LaneStateStrip,
  LiveTradingFreezeNote,
} from '@/components/delivery/LaneDetailShell'
import { LaneOperateSplit } from '@/components/delivery/LaneOperateSplit'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { ReleaseStepCommandCenter } from '@/components/delivery/ReleaseStepCommandCenter'
import {
  runStepStatus,
  gateStepStatus,
  type FlowStep,
} from '@/lib/delivery/releaseStepTypes'
import { ReleaseStateBanner } from '@/components/delivery/ReleaseStateBanner'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { StgTierBChecklistPanel } from '@/components/delivery/StgTierBChecklistPanel'
import { SupplyChainPanel } from '@/components/delivery/SupplyChainPanel'
import { TradeEnvAccessBar } from '@/components/delivery/TradeEnvAccessBar'
import { PlatformGateHistorySection } from '@/components/promote/PlatformReleaseGateSection'
import { ReleaseGateCompareSection } from '@/components/promote/ReleaseGateCompareSection'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { useLaneStepFocus } from '@/hooks/useLaneStepFocus'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildTradeDeployPrompt,
  TRADE_DEPLOY_SCOPE,
} from '@/lib/agent/tradeDeployAgentPrompt'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'

const AI_DEPLOY_LABEL = 'AI Deploy'
const AI_DEPLOY_TASK_LABEL = scopeToLabel(TRADE_DEPLOY_SCOPE)

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

function SupplyChainSummaryLine({
  data,
  isLoading,
}: {
  data: SupplyChainResponse | undefined
  isLoading: boolean
}) {
  if (isLoading) return <span className="text-dense-meta text-muted-foreground">Supply chain…</span>
  const repos = data?.tracked_repos ?? []
  const cms = data?.dockerfile_configmaps?.filter(cm => cm.present).length ?? 0
  const cmTotal = data?.dockerfile_configmaps?.length ?? 0
  return (
    <span className="inline-flex flex-wrap items-center gap-2 text-dense-meta text-muted-foreground">
      <DenseTag variant={repos.length >= 7 ? 'success' : 'warning'}>
        {repos.length} Gitea repos
      </DenseTag>
      <DenseTag variant={cms === cmTotal && cmTotal > 0 ? 'success' : 'warning'}>
        Dockerfile CMs {cms}/{cmTotal}
      </DenseTag>
    </span>
  )
}

type TradeReleasePageProps = {
  context: OpsContextResponse | undefined
  isLoading?: boolean
  onOpenPlacement?: () => void
  /** Compact evidence deep links — health is computed on those surfaces, not here. */
  onOpenSatelliteBus?: () => void
  onOpenObservability?: () => void
  onOpenApiHealth?: () => void
} & AmbientAgentShellProps

export function TradeReleasePage({
  context,
  isLoading = false,
  onOpenPlacement,
  onOpenSatelliteBus,
  onOpenObservability,
  onOpenApiHealth,
  ambientJobId,
  onStartAgentJob,
}: TradeReleasePageProps) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)

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
  const gitops = useQuery({
    queryKey: ['gitops', 'apps'],
    queryFn: fetchGitOpsApps,
    refetchInterval: 30_000,
  })
  const supplyChain = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 30_000,
  })

  const stgDeploy = runStepStatus(stgRuns.data?.runs?.[0])
  const prodDeploy = runStepStatus(prodRuns.data?.runs?.[0])
  const stgGateStep = gateStepStatus(stgGate.data)
  const prodGateStep = gateStepStatus(prodGate.data)

  const [activeIndex, setActiveIndex] = useLaneStepFocus({
    statuses: [stgDeploy.status, stgGateStep.status, prodDeploy.status, prodGateStep.status],
    ready:
      !stgRuns.isLoading && !prodRuns.isLoading && !stgGate.isLoading && !prodGate.isLoading,
    reason: detailReason,
  })

  const aiDeploy = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: TRADE_DEPLOY_SCOPE,
    label: AI_DEPLOY_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildTradeDeployPrompt({
        stgRun: stgRuns.data?.runs?.[0],
        prodRun: prodRuns.data?.runs?.[0],
        stgGate: stgGate.data,
        prodGate: prodGate.data,
        stgSmoke: stgSmoke.data,
        tierB: tierB.data,
        supplyChain: supplyChain.data,
        operatorSurface: 'Deploy Satellite page',
      }),
    }),
  })

  if (isLoading || !context) {
    return <p className="text-muted-foreground">Loading release context…</p>
  }

  const steps: FlowStep[] = [
    { key: 'stg-deploy', label: 'Staging Deploy', env: 'STG', status: stgDeploy.status, statusLabel: stgDeploy.label },
    { key: 'stg-gate', label: 'Staging Gate', env: 'STG', status: stgGateStep.status, statusLabel: stgGateStep.label },
    { key: 'prod-deploy', label: 'Production Deploy', env: 'PROD', status: prodDeploy.status, statusLabel: prodDeploy.label },
    { key: 'prod-gate', label: 'Production Gate', env: 'PROD', status: prodGateStep.status, statusLabel: prodGateStep.label },
  ]

  let stepDetail: ReactNode
  switch (activeIndex) {
    case 0:
      stepDetail = <DeliveryActiveRunPanel target={TRADE_STG_TARGET} collapsible />
      break
    case 1:
      stepDetail = (
        <>
          <DeliveryActiveRunPanel target={TRADE_STG_TARGET} collapsible />
          <StgSmokePanel
            data={stgSmoke.data}
            isLoading={stgSmoke.isLoading}
            isFetching={stgSmoke.isFetching}
            errorMessage={stgSmoke.error instanceof Error ? stgSmoke.error.message : null}
            onRefresh={() => void stgSmoke.refetch()}
            title="STG HTTP smoke"
            description="Post-deliver acceptance via trade-stg gateway."
            collapsible
          />
          <StgTierBChecklistPanel
            tierB={tierB.data}
            tierBLoading={tierB.isLoading}
            layout="observe"
            collapsible
          />
        </>
      )
      break
    case 2:
      stepDetail = (
        <>
          <LiveTradingFreezeNote />
          <DeliveryActiveRunPanel target={TRADE_PROD_TARGET} collapsible />
          <LaneDetailCollapse
            title="Deliver readiness · Trade PROD"
            defaultOpen={false}
            bodyClassName="p-3"
          >
            <PlatformDeliverActuatePanel target={TRADE_PROD_TARGET} hideActions />
          </LaneDetailCollapse>
          <LaneDetailCollapse
            title="Pipeline topology · observe"
            defaultOpen={false}
            bodyClassName="p-3"
          >
            <PipelineRunsPanel
              pipelines={pipelines.data}
              pipelinesLoading={pipelines.isLoading}
              errorMessage={pipelines.error instanceof Error ? pipelines.error.message : null}
              layout="observe"
              onOpenPlacement={onOpenPlacement}
            />
          </LaneDetailCollapse>
        </>
      )
      break
    default:
      stepDetail = (
        <>
          <LiveTradingFreezeNote />
          <DeliveryActiveRunPanel target={TRADE_PROD_TARGET} collapsible />
          <LaneDetailCollapse
            key="prod-gate-detail"
            title="Gate check detail — STG vs Prod"
            summaryExtra={<LaneGateSummaryLine gate={prodGate.data} />}
            defaultOpen={prodGateStep.status === 'error'}
            showModeBadge
            bodyClassName="p-3"
          >
            <ReleaseGateCompareSection
              stgGate={stgGate.data}
              stgGateLoading={stgGate.isLoading}
              stgGateError={stgGate.error instanceof Error ? stgGate.error.message : null}
              prodGate={prodGate.data}
              prodGateLoading={prodGate.isLoading}
              prodGateError={prodGate.error instanceof Error ? prodGate.error.message : null}
            />
          </LaneDetailCollapse>
        </>
      )
      break
  }

  const evidenceLinks =
    onOpenSatelliteBus != null || onOpenObservability != null || onOpenApiHealth != null ? (
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
          Evidence
        </span>
        {onOpenSatelliteBus != null && (
          <Button size="xs" variant="ghost" onClick={onOpenSatelliteBus}>
            Bus Status
          </Button>
        )}
        {onOpenObservability != null && (
          <Button size="xs" variant="ghost" onClick={onOpenObservability}>
            Observability
          </Button>
        )}
        {onOpenApiHealth != null && (
          <Button size="xs" variant="ghost" onClick={onOpenApiHealth}>
            API & Auth Probes
          </Button>
        )}
      </div>
    ) : null

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {aiDeploy.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiDeploy.error.message}</p>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Satellite"
        actions={
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-x-2 gap-y-1">
            <AgentTriggerButton
              className="shrink-0"
              label={AI_DEPLOY_LABEL}
              pending={aiDeploy.isPending}
              disabled={aiDeploy.disabled}
              title={aiDeploy.disabledReason ?? AI_DEPLOY_LABEL}
              onClick={() => aiDeploy.trigger()}
            />
            {evidenceLinks}
          </div>
        }
      >
        <TradeEnvAccessBar />
        <ReleaseStateBanner tier="trade" />
      </LaneStateStrip>

      <LaneOperateSplit
        storageKey="bifrost.console.satelliteLaneOperateSplit"
        primary={
          <>
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
              collapsibleBody
            />
            <div className="flex flex-col gap-3">{stepDetail}</div>
          </>
        }
        support={
          <>
            <LaneDetailCollapse
              title="Supporting evidence"
              summaryExtra={
                <SupplyChainSummaryLine
                  data={supplyChain.data}
                  isLoading={supplyChain.isLoading}
                />
              }
              defaultOpen={false}
              showModeBadge
              bodyClassName="flex flex-col gap-4 p-3"
            >
              <SupplyChainPanel layout="operate" hideDeliverAction />
              <PlatformGateHistorySection
                stgTier="stg"
                prodTier="prod"
                description="Chronological log of Trade release gate runs."
                collapsible
                defaultCollapsed
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Toolbox"
              defaultOpen={false}
              bodyClassName="flex flex-col gap-4 p-3"
            >
              <div className="flex flex-col gap-2">
                <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                  GitOps · sync and rollback
                </span>
                <GitOpsQuickActionsPanel
                  data={gitops.data}
                  isLoading={gitops.isLoading}
                  errorMessage={gitops.error instanceof Error ? gitops.error.message : null}
                />
              </div>
              <div className="flex flex-col gap-2">
                <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                  CI/CD pipeline topology and release workflow
                </span>
                <DeliveryReleaseWorkflowPanel context={context} stgSmoke={stgSmoke.data} />
                <DeliveryFlow context={context} gitops={gitops.data} />
              </div>
            </LaneDetailCollapse>
          </>
        }
      />
    </div>
  )
}
