import { SegmentControl } from '@bifrost/ui'
import { useState } from 'react'
import type {
  DeliveryPipelinesResponse,
  GitOpsAppsResponse,
  OpsContextResponse,
  StackAddonsResponse,
  StgSmokeResponse,
  TierBStatusResponse,
} from '@/api/types'
import { DeliveryObserveOverview } from '@/components/delivery/DeliveryObserveOverview'
import { GitOpsProbePanel } from '@/components/delivery/GitOpsProbePanel'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { StgTierBChecklistPanel } from '@/components/delivery/StgTierBChecklistPanel'
import { StackInstallWizardPanel } from '@/components/delivery/StackInstallWizardPanel'
import { StackAddonsPanel } from '@/components/delivery/StackAddonsPanel'
import { SupplyChainPanel } from '@/components/delivery/SupplyChainPanel'
import {
  DEFAULT_DELIVERY_OBSERVE_SECTION,
  DELIVERY_OBSERVE_SECTIONS,
  type DeliveryObserveSection,
} from '@/lib/delivery/deliveryObserveSections'

interface DeliveryObserveViewProps {
  context: OpsContextResponse
  gitops?: GitOpsAppsResponse
  gitopsLoading?: boolean
  gitopsError?: string | null
  stack?: StackAddonsResponse
  stackLoading?: boolean
  stackError?: string | null
  showStackWizard?: boolean
  pipelines?: DeliveryPipelinesResponse
  pipelinesLoading?: boolean
  pipelinesError?: string | null
  stgSmoke?: StgSmokeResponse
  stgSmokeLoading?: boolean
  stgSmokeFetching?: boolean
  stgSmokeError?: string | null
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  tierBLoading?: boolean
  onRefreshStgSmoke?: () => void
  onOpenAudit: () => void
  onOpenPlacement?: () => void
  onDrawerOpenChange?: (open: boolean) => void
}

export function DeliveryObserveView({
  context,
  gitops,
  gitopsLoading = false,
  gitopsError = null,
  stack,
  stackLoading = false,
  stackError = null,
  showStackWizard = false,
  pipelines,
  pipelinesLoading = false,
  pipelinesError = null,
  stgSmoke,
  stgSmokeLoading = false,
  stgSmokeFetching = false,
  stgSmokeError = null,
  lastDeliverSucceeded = false,
  tierB,
  tierBLoading = false,
  onRefreshStgSmoke,
  onOpenAudit,
  onOpenPlacement,
  onDrawerOpenChange,
}: DeliveryObserveViewProps) {
  const [section, setSection] = useState<DeliveryObserveSection>(DEFAULT_DELIVERY_OBSERVE_SECTION)
  const sectionMeta = DELIVERY_OBSERVE_SECTIONS.find(s => s.value === section)

  return (
    <div className="delivery-observe-view">
      <div className="delivery-observe-view__nav">
        <SegmentControl
          value={section}
          onChange={v => setSection(v as DeliveryObserveSection)}
          options={DELIVERY_OBSERVE_SECTIONS.map(s => ({ value: s.value, label: s.label }))}
          size="sm"
        />
        {sectionMeta?.hint != null && sectionMeta.hint !== '' && (
          <p className="delivery-observe-view__hint">{sectionMeta.hint}</p>
        )}
      </div>

      <div className="delivery-observe-view__panels">
        {section === 'overview' && (
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
        )}

        {section === 'stg' && (
          <>
            <StgSmokePanel
              data={stgSmoke}
              isLoading={stgSmokeLoading}
              isFetching={stgSmokeFetching}
              errorMessage={stgSmokeError}
              onRefresh={onRefreshStgSmoke}
              title="Verify STG"
              description="Post-deliver HTTP acceptance via Traefik gateway (trade-stg.bifrost.lan). Run deliver on Operate, then refresh here."
            />
            <StgTierBChecklistPanel tierB={tierB} tierBLoading={tierBLoading} layout="observe" />
          </>
        )}

        {section === 'stack' && (
          <>
            <GitOpsProbePanel
              data={gitops}
              isLoading={gitopsLoading}
              errorMessage={gitopsError}
              layout="observe"
              onOpenAudit={onOpenAudit}
              onDrawerOpenChange={onDrawerOpenChange}
            />
            {showStackWizard && (
              <StackInstallWizardPanel
                data={stack}
                isLoading={stackLoading}
                errorMessage={stackError}
                layout="observe"
              />
            )}
            <StackAddonsPanel
              data={stack}
              isLoading={stackLoading}
              errorMessage={stackError}
              layout="observe"
            />
          </>
        )}

        {section === 'history' && (
          <>
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
      </div>
    </div>
  )
}
