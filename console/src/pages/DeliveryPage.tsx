import { useState } from 'react'
import type { ClusterSummary, DeliveryPipelinesResponse, GitOpsAppsResponse, MatrixResponse, OpsContextResponse, ReleaseGateResponse, StackAddonsResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/types'
import { DeliveryOperateView } from '@/components/delivery/DeliveryOperateView'
import { DeliveryObserveView } from '@/components/delivery/DeliveryObserveView'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { DeliveryReleaseWorkflowPanel } from '@/components/delivery/DeliveryReleaseWorkflowPanel'
import { GovernancePhase2SignoffPanel } from '@/components/architecture/GovernancePhase2SignoffPanel'
import { DeliveryViewShell } from '@/components/delivery/DeliveryViewShell'
import { ProdDeliverPanel } from '@/components/delivery/ProdDeliverPanel'
import { StackInstallWizardPanel } from '@/components/delivery/StackInstallWizardPanel'
import { OpsSection } from '@/components/layout/OpsSection'
import { evaluateStgReleaseStatus } from '@/lib/control-room/matrixSummary'
import {
  ciModeLabel,
  DELIVERY_STRATEGY_BULLETS,
  showGitOpsPlannedBadge,
} from '@/lib/delivery/deliveryPhase'
import {
  DEFAULT_DELIVERY_PAGE_TAB,
  type DeliveryPageTab,
} from '@/lib/delivery/deliveryPageTabs'
import { stackNeedsOperatePanel } from '@/lib/delivery/stackWizard'

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
  const stackAddons = stack?.addons ?? []
  const showStackOperate =
    !stackLoading && stack != null && stackNeedsOperatePanel(stackAddons)

  return (
    <div
      className={`flex w-full min-w-0 flex-col${gitOpsDrawerOpen ? ' cluster-page-shell--node-drawer' : ''}`}
    >
      <DeliveryViewShell
        pageTab={pageTab}
        onPageTabChange={setPageTab}
        context={context}
        ciMode={ciMode}
        gitOpsPlanned={gitOpsPlanned}
        stgReleaseReady={stgRelease.releaseReady}
        stgSmokeFails={stgRelease.smokeFails}
      >
        {pageTab === 'operate' && (
          <>
            <DeliveryOperateView
              gitops={gitops}
              gitopsLoading={gitopsLoading}
              gitopsError={gitopsError}
              onOpenObserve={() => setPageTab('observe')}
              context={context}
              matrices={matrices}
              stgSmoke={stgSmoke}
              lastDeliverSucceeded={lastDeliverSucceeded}
              stgGate={stgGate}
              tierB={tierB}
              tierBLoading={tierBLoading}
              onOpenPromote={onOpenPromote}
              onOpenMilestones={onOpenMilestones}
              onOpenAudit={onOpenAudit}
            />
            {showStackOperate && (
              <StackInstallWizardPanel
                data={stack}
                isLoading={stackLoading}
                errorMessage={stackError}
                layout="operate"
              />
            )}
          </>
        )}

        {pageTab === 'observe' && (
          <DeliveryObserveView
            context={context}
            gitops={gitops}
            gitopsLoading={gitopsLoading}
            gitopsError={gitopsError}
            stack={stack}
            stackLoading={stackLoading}
            stackError={stackError}
            showStackWizard={showStackOperate}
            pipelines={pipelines}
            pipelinesLoading={pipelinesLoading}
            pipelinesError={pipelinesError}
            stgSmoke={stgSmoke}
            stgSmokeLoading={stgSmokeLoading}
            stgSmokeFetching={stgSmokeFetching}
            stgSmokeError={stgSmokeError}
            lastDeliverSucceeded={lastDeliverSucceeded}
            tierB={tierB}
            tierBLoading={tierBLoading}
            onRefreshStgSmoke={onRefreshStgSmoke}
            onOpenAudit={onOpenAudit}
            onOpenPlacement={onOpenPlacement}
            onDrawerOpenChange={setGitOpsDrawerOpen}
          />
        )}

        {pageTab === 'blueprint' && (
          <>
            <GovernancePhase2SignoffPanel />
            <DeliveryReleaseWorkflowPanel
              context={context}
              stgSmoke={stgSmoke}
              lastDeliverSucceeded={lastDeliverSucceeded}
              onOpenMilestones={onOpenMilestones}
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
        </>
        )}
      </DeliveryViewShell>
    </div>
  )
}
