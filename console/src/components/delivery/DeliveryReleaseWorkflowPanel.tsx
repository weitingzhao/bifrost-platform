import { useMemo } from 'react'
import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { OpsContextResponse, StgSmokeResponse } from '@/api/types'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import { evaluateStgDeliverStatus } from '@/lib/control-room/matrixSummary'
import {
  buildStgReleasePhases,
  DELIVERY_PIPELINE_CATALOG,
  DELIVERY_RUNBOOK_COMMANDS,
  type DeliveryPhaseStatus,
} from '@/lib/architecture/deliveryMainlineCatalog'

function phaseLamp(status: DeliveryPhaseStatus): 'ok' | 'fail' | 'degraded' | 'unknown' {
  switch (status) {
    case 'done':
      return 'ok'
    case 'active':
      return 'degraded'
    case 'blocked':
      return 'fail'
    default:
      return 'unknown'
  }
}

function phaseStatusTagVariant(status: DeliveryPhaseStatus): 'success' | 'warning' | 'danger' | 'neutral' {
  switch (status) {
    case 'done':
      return 'success'
    case 'active':
      return 'warning'
    case 'blocked':
      return 'danger'
    default:
      return 'neutral'
  }
}

interface DeliveryReleaseWorkflowPanelProps {
  context: OpsContextResponse
  stgSmoke?: StgSmokeResponse
  lastDeliverSucceeded?: boolean
  onOpenMilestones?: () => void
}

export function DeliveryReleaseWorkflowPanel({
  context,
  stgSmoke,
  lastDeliverSucceeded = false,
  onOpenMilestones,
}: DeliveryReleaseWorkflowPanelProps) {
  const phases = useMemo(() => buildStgReleasePhases(context), [context])
  const stgSmokeStatus = evaluateStgDeliverStatus(stgSmoke, lastDeliverSucceeded)

  const stgTrackDone = phases.filter(p => p.id !== 'prod-cutover').every(p => p.status === 'done')
  const prodPhase = phases.find(p => p.id === 'prod-cutover')

  return (
    <OpsSection
      title="STG release workflow"
      description="Prepare → Deliver → Verify → STG gate. Prod cutover is a separate track (Deploy Mainline / D1). Phase status from spine (Projection)."
      bodyPadding="default"
      overflow="visible"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-meta)]">
          <StatusLamp value={stgTrackDone ? 'ok' : 'degraded'} kind="reach" />
          <span className="font-medium text-[var(--text-dense-label)]">
            {stgTrackDone ? 'STG deliver track complete (spine)' : 'STG deliver track in progress (spine)'}
          </span>
        </span>
        {prodPhase != null && (
          <DenseTag variant={phaseStatusTagVariant(prodPhase.status)}>
            Prod cutover · {prodPhase.status}
          </DenseTag>
        )}
        {onOpenMilestones != null && (
          <button
            type="button"
            className="text-[var(--text-dense-caption)] text-[var(--primary)] underline-offset-2 hover:underline"
            onClick={onOpenMilestones}
          >
            Open Milestones
          </button>
        )}
        {!stgSmokeStatus.ready && stgSmokeStatus.reasons.length > 0 && (
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            Live smoke: {stgSmokeStatus.reasons.join(' · ')}
          </span>
        )}
      </div>

      <OpsSubsectionTitle>Pipelines</OpsSubsectionTitle>
      <ul className="m-0 mb-4 list-none space-y-1 p-0 text-[var(--text-dense-meta)]">
        {DELIVERY_PIPELINE_CATALOG.map(p => (
          <li key={p.name} className="flex flex-wrap items-center gap-2">
            <code className="text-[var(--text-dense-label)]">{p.name}</code>
            {p.legacy ? (
              <DenseTag variant="neutral">legacy</DenseTag>
            ) : p.tier === 'primary' ? (
              <DenseTag variant="success">primary</DenseTag>
            ) : (
              <DenseTag variant="category">aux</DenseTag>
            )}
            <span className="text-[var(--muted-foreground)]">{p.purpose}</span>
          </li>
        ))}
      </ul>

      <OpsSubsectionTitle>Phases (Projection ← spine)</OpsSubsectionTitle>
      <ol className="m-0 list-none space-y-3 p-0">
        {phases.map(phase => (
          <li
            key={phase.id}
            className="rounded-md border border-[var(--border)] bg-[var(--secondary)]/40 px-3 py-2"
          >
            <div className="flex flex-wrap items-center gap-2">
              <StatusLamp value={phaseLamp(phase.status)} kind="reach" />
              <span className="font-medium text-[var(--text-dense-label)]">
                {phase.seq}. {phase.title}
              </span>
              <DenseTag variant="category">{phase.owner}</DenseTag>
              <DenseTag variant={phaseStatusTagVariant(phase.status)}>{phase.status}</DenseTag>
              {phase.spineMilestoneId != null && phase.spineStatus != null && (
                <DenseTag variant="neutral" className="font-mono-tabular text-[10px]">
                  {phase.spineMilestoneId} · {phase.spineStatus}
                </DenseTag>
              )}
            </div>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {phase.summary}
            </p>
            <ul className="m-0 mt-1 list-disc px-4 text-[var(--text-dense-meta)]">
              {phase.actions.map(a => (
                <li key={a}>{a}</li>
              ))}
            </ul>
          </li>
        ))}
      </ol>

      <OpsSubsectionTitle className="mt-4">CLI shortcuts</OpsSubsectionTitle>
      <ul className="m-0 list-disc px-5 text-[var(--text-dense-meta)] font-mono text-[11px]">
        <li>{DELIVERY_RUNBOOK_COMMANDS.deliver}</li>
        <li>{DELIVERY_RUNBOOK_COMMANDS.mirrorSync}</li>
        <li>{DELIVERY_RUNBOOK_COMMANDS.syncConfig}</li>
        <li>{DELIVERY_RUNBOOK_COMMANDS.verify}</li>
      </ul>
    </OpsSection>
  )
}
