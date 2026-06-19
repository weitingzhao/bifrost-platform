import type { ReactNode } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import type {
  DeliveryPipelinesResponse,
  GitOpsAppsResponse,
  MatrixResponse,
  OpsContextResponse,
  ReleaseGateResponse,
  StackAddonsResponse,
  StgSmokeResponse,
  TierBStatusResponse,
} from '@/api/types'
import { AuditPageLink } from '@/components/AuditPageLink'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  evaluatePromoteStatus,
  evaluateStgReleaseStatus,
} from '@/lib/control-room/matrixSummary'
import { ciModeLabel, showGitOpsPlannedBadge } from '@/lib/delivery/deliveryPhase'

interface DeliveryObserveOverviewProps {
  context: OpsContextResponse
  stgSmoke?: StgSmokeResponse
  stgSmokeLoading?: boolean
  lastDeliverSucceeded?: boolean
  gitops?: GitOpsAppsResponse
  gitopsLoading?: boolean
  stack?: StackAddonsResponse
  stackLoading?: boolean
  pipelines?: DeliveryPipelinesResponse
  pipelinesLoading?: boolean
}

function OverviewCell({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2">
      <p className="m-0 text-[var(--text-dense-caption)] font-medium uppercase tracking-wide text-[var(--muted-foreground)]">
        {label}
      </p>
      <div className="mt-1.5">{children}</div>
    </div>
  )
}

export function DeliveryObserveOverview({
  context,
  stgSmoke,
  stgSmokeLoading = false,
  lastDeliverSucceeded = false,
  gitops,
  gitopsLoading = false,
  stack,
  stackLoading = false,
  pipelines,
  pipelinesLoading = false,
}: DeliveryObserveOverviewProps) {
  const stgRelease = evaluateStgReleaseStatus(stgSmoke, lastDeliverSucceeded)
  const gitOpsLamp =
    gitops?.reachability === 'ok' && gitops.argocd_status === 'installed'
      ? 'ok'
      : gitops?.reachability === 'fail' || gitops?.argocd_status === 'unavailable'
        ? 'fail'
        : gitops != null
          ? 'degraded'
          : 'unknown'
  const stackLamp =
    stack?.reachability === 'ok' ? 'ok' : stack?.reachability === 'fail' ? 'fail' : 'degraded'
  const pipelineLamp =
    pipelines?.reachability === 'ok' ? 'ok' : pipelines?.reachability === 'fail' ? 'fail' : 'degraded'
  const ciMode = ciModeLabel(context.deployment.phase)
  const gitOpsPlanned = showGitOpsPlannedBadge(context.deployment)

  return (
    <OpsSection
      title="Status overview"
      description="STG health, CI/CD stack probes, and delivery readiness at a glance."
      bodyPadding="default"
      overflow="visible"
    >
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <OverviewCell label="STG smoke">
          {stgSmokeLoading ? (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Probing…</span>
          ) : (
            <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-meta)]">
              <StatusLamp value={stgSmoke?.reachability ?? 'unknown'} kind="reach" />
              <span>{stgRelease.ready ? 'Deliver + smoke OK' : stgRelease.reasons[0] ?? stgSmoke?.detail ?? '—'}</span>
            </span>
          )}
        </OverviewCell>
        <OverviewCell label="GitOps">
          {gitopsLoading ? (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading…</span>
          ) : (
            <span className="inline-flex flex-wrap items-center gap-1.5 text-[var(--text-dense-meta)]">
              <StatusLamp value={gitOpsLamp} kind="reach" />
              <span>{gitops?.detail ?? 'Not probed'}</span>
              {gitOpsPlanned && <DenseTag variant="neutral">planned</DenseTag>}
            </span>
          )}
        </OverviewCell>
        <OverviewCell label="CI/CD stack">
          {stackLoading || pipelinesLoading ? (
            <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">Loading…</span>
          ) : (
            <span className="inline-flex flex-wrap items-center gap-1.5 text-[var(--text-dense-meta)]">
              <StatusLamp value={stackLamp} kind="reach" />
              <span>{stack?.detail ?? '—'}</span>
              <StatusLamp value={pipelineLamp} kind="reach" />
              <span>{pipelines?.pipelines.length ?? 0} pipelines</span>
            </span>
          )}
        </OverviewCell>
        <OverviewCell label="Deployment phase">
          <div className="flex flex-wrap items-center gap-2">
            <DenseTag variant="category" className="font-mono-tabular">
              {context.deployment.phase}
            </DenseTag>
            <DenseTag variant="category">{ciMode}</DenseTag>
          </div>
        </OverviewCell>
      </div>
    </OpsSection>
  )
}

interface DeliveryCouplingGatePanelProps {
  context: OpsContextResponse
  matrices: MatrixResponse[]
  stgSmoke?: StgSmokeResponse
  lastDeliverSucceeded?: boolean
  stgGate?: ReleaseGateResponse
  tierB?: TierBStatusResponse
  onOpenPromote: () => void
  onOpenMilestones: () => void
  onOpenAudit: () => void
}

export function DeliveryCouplingGatePanel({
  context,
  matrices,
  stgSmoke,
  lastDeliverSucceeded = false,
  stgGate,
  tierB,
  onOpenPromote,
  onOpenMilestones,
  onOpenAudit,
}: DeliveryCouplingGatePanelProps) {
  const promote = evaluatePromoteStatus(context, matrices)
  const stgRelease = evaluateStgReleaseStatus(stgSmoke, lastDeliverSucceeded, stgGate, tierB)
  const stgLamp = stgRelease.releaseReady ? 'ok' : stgRelease.smokeFails ? 'fail' : 'degraded'
  const prodLamp = promote.ready ? 'ok' : promote.blockedByDecision || promote.prodFails ? 'fail' : 'degraded'

  return (
    <OpsSection
      title="Coupling gate"
      leading={<StatusLamp value={prodLamp} kind="reach" />}
      description="STG release track vs Prod cutover track — run gates on Promote."
      bodyPadding="default"
      overflow="visible"
      className="coupling-gate-panel"
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2">
          <p className="m-0 flex items-center gap-2 font-medium text-[var(--text-dense-label)]">
            <StatusLamp value={stgLamp} kind="reach" />
            STG release
          </p>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {stgRelease.releaseReady
              ? 'Deliver + smoke + STG gate + Tier B'
              : stgRelease.releaseReasons.join(' · ')}
          </p>
        </div>
        <div className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2">
          <p className="m-0 flex items-center gap-2 font-medium text-[var(--text-dense-label)]">
            <StatusLamp value={prodLamp} kind="reach" />
            Prod cutover
          </p>
          <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {promote.ready ? 'Narrative ready' : promote.reasons.join(' · ')}
          </p>
        </div>
      </div>
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
  )
}
