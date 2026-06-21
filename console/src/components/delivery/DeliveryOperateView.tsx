import type {
  GitOpsAppsResponse,
  MatrixResponse,
  OpsContextResponse,
  ReleaseGateResponse,
  StgSmokeResponse,
  TierBStatusResponse,
} from '@/api/types'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeliveryCouplingGatePanel } from '@/components/delivery/DeliveryObserveOverview'
import { GitOpsQuickActionsPanel } from '@/components/delivery/GitOpsQuickActionsPanel'
import { StgTierBChecklistPanel } from '@/components/delivery/StgTierBChecklistPanel'
import { SupplyChainPanel } from '@/components/delivery/SupplyChainPanel'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'

interface DeliveryOperateViewProps {
  gitops?: GitOpsAppsResponse
  gitopsLoading?: boolean
  gitopsError?: string | null
  onOpenObserve: () => void
  context: OpsContextResponse
  matrices: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  lastDeliverSucceeded?: boolean
  stgGate?: ReleaseGateResponse
  tierB?: TierBStatusResponse
  tierBLoading?: boolean
  onOpenPromote: () => void
  onOpenMilestones: () => void
  onOpenAudit: () => void
}

const TRADE_TARGET = deliveryTargetById('trade-stg')

export function DeliveryOperateView({
  gitops,
  gitopsLoading = false,
  gitopsError = null,
  onOpenObserve,
  context,
  matrices,
  stgSmoke,
  lastDeliverSucceeded = false,
  stgGate,
  tierB,
  tierBLoading = false,
  onOpenPromote,
  onOpenMilestones,
  onOpenAudit,
}: DeliveryOperateViewProps) {
  return (
    <div className="flex flex-col gap-4">
      <SupplyChainPanel layout="operate" />
      <DeliveryActiveRunPanel target={TRADE_TARGET} />
      <StgTierBChecklistPanel
        tierB={tierB}
        tierBLoading={tierBLoading}
        onOpenPromote={onOpenPromote}
        layout="operate"
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
      <GitOpsQuickActionsPanel
        data={gitops}
        isLoading={gitopsLoading}
        errorMessage={gitopsError}
        onOpenObserve={onOpenObserve}
      />
    </div>
  )
}
