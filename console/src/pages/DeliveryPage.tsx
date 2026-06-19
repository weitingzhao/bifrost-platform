import { useState } from 'react'
import { DenseTag, SegmentControl, StatusLamp } from '@bifrost/ui'
import type { ClusterSummary, DeliveryPipelinesResponse, GitOpsAppsResponse, MatrixResponse, OpsContextResponse, ReleaseGateResponse, StackAddonsResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/types'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeliveryCouplingGatePanel, DeliveryObserveOverview } from '@/components/delivery/DeliveryObserveOverview'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { DeliveryReleaseWorkflowPanel } from '@/components/delivery/DeliveryReleaseWorkflowPanel'
import { GitOpsProbePanel } from '@/components/delivery/GitOpsProbePanel'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { ProdDeliverPanel } from '@/components/delivery/ProdDeliverPanel'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { StgTierBChecklistPanel } from '@/components/delivery/StgTierBChecklistPanel'
import { StackAddonsPanel } from '@/components/delivery/StackAddonsPanel'
import { SupplyChainPanel } from '@/components/delivery/SupplyChainPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { evaluateStgReleaseStatus } from '@/lib/control-room/matrixSummary'
import {
  ciModeLabel,
  DELIVERY_STRATEGY_BULLETS,
  showGitOpsPlannedBadge,
} from '@/lib/delivery/deliveryPhase'
import {
  DEFAULT_DELIVERY_PAGE_TAB,
  DELIVERY_PAGE_TABS,
  type DeliveryPageTab,
} from '@/lib/delivery/deliveryPageTabs'

interface DeliveryPageProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary?: ClusterSummary
  gitops?: GitOpsAppsResponse
  gitopsLoading?: boolean
  gitopsError?: string | null
  stack?: StackAddonsResponse
  stackLoading?: boolean
  stackError?: string | null
  pipelines?: DeliveryPipelinesResponse
  pipelinesLoading?: boolean
  pipelinesError?: string | null
  stgSmoke?: StgSmokeResponse
  stgSmokeLoading?: boolean
  stgSmokeFetching?: boolean
  stgSmokeError?: string | null
  lastDeliverSucceeded?: boolean
  stgGate?: ReleaseGateResponse
  tierB?: TierBStatusResponse
  tierBLoading?: boolean
  onRefreshStgSmoke?: () => void
  isLoading: boolean
  onOpenMilestones: () => void
  onOpenPromote: () => void
  onOpenAudit: () => void
  onOpenPlacement?: () => void
  onOpenDeployMainline?: () => void
}

export function DeliveryPage({
  context,
  matrices,
  clusterSummary,
  gitops,
  gitopsLoading = false,
  gitopsError = null,
  stack,
  stackLoading = false,
  stackError = null,
  pipelines,
  pipelinesLoading = false,
  pipelinesError = null,
  stgSmoke,
  stgSmokeLoading = false,
  stgSmokeFetching = false,
  stgSmokeError = null,
  lastDeliverSucceeded = false,
  stgGate,
  tierB,
  tierBLoading = false,
  onRefreshStgSmoke,
  isLoading,
  onOpenMilestones,
  onOpenPromote,
  onOpenAudit,
  onOpenPlacement,
  onOpenDeployMainline,
}: DeliveryPageProps) {
  const [pageTab, setPageTab] = useState<DeliveryPageTab>(DEFAULT_DELIVERY_PAGE_TAB)
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)
  const [gitOpsDrawerOpen, setGitOpsDrawerOpen] = useState(false)

  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading delivery context…</p>
  }

  const stgRelease = evaluateStgReleaseStatus(stgSmoke, lastDeliverSucceeded, stgGate, tierB)
  const ciMode = ciModeLabel(context.deployment.phase)
  const gitOpsPlanned = showGitOpsPlannedBadge(context.deployment)
  const activeTabMeta = DELIVERY_PAGE_TABS.find(t => t.value === pageTab)

  return (
    <div
      className={`flex w-full min-w-0 flex-col gap-4${gitOpsDrawerOpen ? ' cluster-page-shell--node-drawer' : ''}`}
    >
      <OpsSection
        title="Delivery"
        description={activeTabMeta?.hint ?? 'STG CI/CD — operate, observe, or review blueprint.'}
        bodyPadding="default"
        overflow="visible"
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
          <SegmentControl
            ariaLabel="Delivery view"
            value={pageTab}
            onChange={v => setPageTab(v as DeliveryPageTab)}
            options={DELIVERY_PAGE_TABS.map(t => ({ value: t.value, label: t.label }))}
          />
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant="category" className="font-mono-tabular">
              phase: {context.deployment.phase}
            </DenseTag>
            <DenseTag variant="category">{ciMode}</DenseTag>
            <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-meta)]">
              <StatusLamp
                value={stgRelease.releaseReady ? 'ok' : stgRelease.smokeFails ? 'fail' : 'degraded'}
                kind="reach"
              />
              <span>{stgRelease.releaseReady ? 'STG release ready' : 'STG in progress'}</span>
            </span>
            {gitOpsPlanned && <DenseTag variant="neutral">GitOps planned</DenseTag>}
          </div>
        </div>
      </OpsSection>

      {pageTab === 'operate' && (
        <>
          <SupplyChainPanel layout="operate" />
          <DeliveryActiveRunPanel />
          <GitOpsProbePanel
            data={gitops}
            isLoading={gitopsLoading}
            errorMessage={gitopsError}
            layout="operate"
            onOpenAudit={onOpenAudit}
            onDrawerOpenChange={setGitOpsDrawerOpen}
          />
          <StgSmokePanel
            data={stgSmoke}
            isLoading={stgSmokeLoading}
            isFetching={stgSmokeFetching}
            errorMessage={stgSmokeError}
            onRefresh={onRefreshStgSmoke}
            title="Verify STG"
            description="Post-deliver HTTP acceptance via nginx gateway (:30880). Refresh after pipeline completes."
          />
          <StgTierBChecklistPanel
            tierB={tierB}
            tierBLoading={tierBLoading}
            onOpenPromote={onOpenPromote}
          />
          <PipelineRunsPanel
            pipelines={pipelines}
            pipelinesLoading={pipelinesLoading}
            errorMessage={pipelinesError}
            stgSmokeDetail={stgSmoke?.detail}
            layout="operate-recent"
          />
        </>
      )}

      {pageTab === 'observe' && (
        <>
          <DeliveryObserveOverview
            context={context}
            stgSmoke={stgSmoke}
            stgSmokeLoading={stgSmokeLoading}
            lastDeliverSucceeded={lastDeliverSucceeded}
            gitops={gitops}
            gitopsLoading={gitopsLoading}
            stack={stack}
            stackLoading={stackLoading}
            pipelines={pipelines}
            pipelinesLoading={pipelinesLoading}
          />
          <GitOpsProbePanel
            data={gitops}
            isLoading={gitopsLoading}
            errorMessage={gitopsError}
            layout="observe"
            onOpenAudit={onOpenAudit}
            onDrawerOpenChange={setGitOpsDrawerOpen}
          />
          <StackAddonsPanel data={stack} isLoading={stackLoading} errorMessage={stackError} />
          <StgSmokePanel
            data={stgSmoke}
            isLoading={stgSmokeLoading}
            isFetching={stgSmokeFetching}
            errorMessage={stgSmokeError}
            onRefresh={onRefreshStgSmoke}
          />
          <PipelineRunsPanel
            pipelines={pipelines}
            pipelinesLoading={pipelinesLoading}
            errorMessage={pipelinesError}
            stgSmokeDetail={stgSmoke?.detail}
            onOpenPlacement={onOpenPlacement}
            layout="observe"
          />
          <SupplyChainPanel layout="observe" />
        </>
      )}

      {pageTab === 'blueprint' && (
        <>
          <DeliveryReleaseWorkflowPanel
            stgSmoke={stgSmoke}
            lastDeliverSucceeded={lastDeliverSucceeded}
          />
          <ProdDeliverPanel
            context={context}
            onOpenDeployMainline={onOpenDeployMainline}
            onOpenPromote={onOpenPromote}
          />
          <OpsSection title="Strategy (K3S §5)" bodyPadding="default" overflow="visible">
            <ul className="m-0 list-disc px-5 text-[var(--text-dense)]">
              {DELIVERY_STRATEGY_BULLETS.map(b => (
                <li key={b}>{b}</li>
              ))}
            </ul>
          </OpsSection>
          <DeliveryFlow
            context={context}
            selectionId={selectedNodeId}
            clusterReachOk={clusterSummary?.reachability === 'ok'}
            gitops={gitops}
            stack={stack}
            onSelectNode={id => setSelectedNodeId(prev => (prev === id ? null : id))}
          />
          <DeliveryCouplingGatePanel
            context={context}
            matrices={matrices}
            stgSmoke={stgSmoke}
            lastDeliverSucceeded={lastDeliverSucceeded}
            stgGate={stgGate}
            tierB={tierB}
            onOpenPromote={onOpenPromote}
            onOpenMilestones={onOpenMilestones}
            onOpenAudit={onOpenAudit}
          />
        </>
      )}
    </div>
  )
}
