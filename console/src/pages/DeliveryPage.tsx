import { useState } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import type { ClusterSummary, DeliveryPipelinesResponse, GitOpsAppsResponse, MatrixResponse, OpsContextResponse, StackAddonsResponse, StgSmokeResponse } from '@/api/types'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { GitOpsProbePanel } from '@/components/delivery/GitOpsProbePanel'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { StackAddonsPanel } from '@/components/delivery/StackAddonsPanel'
import { AuditPageLink } from '@/components/AuditPageLink'
import { flywheelLabel } from '@/components/FocusStrip'
import { OpsSection } from '@/components/layout/OpsSection'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'
import {
  ciModeLabel,
  DELIVERY_STRATEGY_BULLETS,
  showGitOpsPlannedBadge,
} from '@/lib/delivery/deliveryPhase'

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
  onRefreshStgSmoke?: () => void
  isLoading: boolean
  onOpenMilestones: () => void
  onOpenPromote: () => void
  onOpenAudit: () => void
  onOpenPlacement?: () => void
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
  onRefreshStgSmoke,
  isLoading,
  onOpenMilestones,
  onOpenPromote,
  onOpenAudit,
  onOpenPlacement,
}: DeliveryPageProps) {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null)

  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading delivery context…</p>
  }

  const promote = evaluatePromoteStatus(context, matrices)
  const lamp = promote.ready ? 'ok' : promote.blockedByDecision || promote.prodFails ? 'fail' : 'degraded'
  const ciMode = ciModeLabel(context.deployment.phase)
  const gitOpsPlanned = showGitOpsPlannedBadge(context.deployment)
  const gitOpsLamp =
    gitops?.reachability === 'ok' && gitops.argocd_status === 'installed'
      ? 'ok'
      : gitops?.reachability === 'fail' || gitops?.argocd_status === 'unavailable'
        ? 'fail'
        : gitops != null
          ? 'degraded'
          : 'unknown'

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="CI/CD path"
        description="Unified view of near-term delivery (Mac CI + compose prod) and target GitOps on K3s. For migration milestones see Milestones; for release readiness see Promote."
        bodyPadding="default"
        overflow="visible"
      >
        <div className="flex flex-wrap items-center gap-2">
          <DenseTag variant="category" className="font-mono-tabular">
            phase: {context.deployment.phase}
          </DenseTag>
          <DenseTag variant="category" className="focus-strip-ci-mode">
            CI: {ciMode}
          </DenseTag>
          <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-meta)]">
            <StatusLamp value={gitOpsLamp} kind="reach" />
            <span>GitOps probe</span>
          </span>
          {gitOpsPlanned && <DenseTag variant="neutral">GitOps planned</DenseTag>}
        </div>
      </OpsSection>

      <GitOpsProbePanel data={gitops} isLoading={gitopsLoading} errorMessage={gitopsError} />

      <StackAddonsPanel data={stack} isLoading={stackLoading} errorMessage={stackError} />

      <PipelineRunsPanel
        pipelines={pipelines}
        pipelinesLoading={pipelinesLoading}
        errorMessage={pipelinesError}
        stgSmokeDetail={stgSmoke?.detail}
        onOpenPlacement={onOpenPlacement}
      />

      <StgSmokePanel
        data={stgSmoke}
        isLoading={stgSmokeLoading}
        isFetching={stgSmokeFetching}
        errorMessage={stgSmokeError}
        onRefresh={onRefreshStgSmoke}
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

      <OpsSection
        title="Coupling gate"
        leading={<StatusLamp value={lamp} kind="reach" />}
        bodyPadding="default"
        overflow="visible"
        className="coupling-gate-panel"
      >
        <p className="m-0 text-[var(--text-dense)] font-medium">
          {promote.ready ? 'Promote ready (narrative)' : 'Promote blocked'}
        </p>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Flywheel primary: {flywheelLabel(context.focus.flywheel_primary)} · Both flywheels must pass
          before production cutover.
        </p>
        {!promote.ready && promote.reasons.length > 0 && (
          <ul className="m-0 mt-2 list-disc px-4 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {promote.reasons.map(r => (
              <li key={r}>{r}</li>
            ))}
          </ul>
        )}
        <div className="mt-3 flex flex-wrap gap-2">
          <Button size="sm" onClick={onOpenPromote}>
            Open Promote
          </Button>
          <Button variant="ghost" size="sm" onClick={onOpenMilestones}>
            Open Milestones
          </Button>
        </div>
        <AuditPageLink
          onOpenAudit={onOpenAudit}
          hint="After GitOps Sync, review actuation history on"
          className="mt-3"
        />
      </OpsSection>
    </div>
  )
}
