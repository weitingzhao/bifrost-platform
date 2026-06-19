import { DenseTag, StatusLamp } from '@bifrost/ui'
import type { StgSmokeResponse } from '@/api/types'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import {
  DELIVERY_PIPELINE_CATALOG,
  DELIVERY_RUNBOOK_COMMANDS,
  STG_RELEASE_PHASES,
  type DeliveryPhaseStatus,
} from '@/lib/architecture/deliveryMainlineCatalog'
import { evaluateStgDeliverStatus } from '@/lib/control-room/matrixSummary'

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

interface DeliveryReleaseWorkflowPanelProps {
  stgSmoke?: StgSmokeResponse
  lastDeliverSucceeded?: boolean
}

export function DeliveryReleaseWorkflowPanel({
  stgSmoke,
  lastDeliverSucceeded = false,
}: DeliveryReleaseWorkflowPanelProps) {
  const stgStatus = evaluateStgDeliverStatus(stgSmoke, lastDeliverSucceeded)

  return (
    <OpsSection
      title="STG release workflow"
      description="Prepare → Deliver → Verify → STG gate. Prod cutover is a separate track (Deploy Mainline / D1)."
      bodyPadding="default"
      overflow="visible"
    >
      <div className="mb-3 flex flex-wrap items-center gap-2">
        <span className="inline-flex items-center gap-1.5 text-[var(--text-dense-meta)]">
          <StatusLamp value={stgStatus.ready ? 'ok' : stgStatus.smokeFails ? 'fail' : 'degraded'} kind="reach" />
          <span className="font-medium text-[var(--text-dense-label)]">
            {stgStatus.ready ? 'STG deliver track ready' : 'STG deliver track in progress'}
          </span>
        </span>
        {!stgStatus.ready && stgStatus.reasons.length > 0 && (
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {stgStatus.reasons.join(' · ')}
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
            ) : p.tier === 'planned' ? (
              <DenseTag variant="warning">planned</DenseTag>
            ) : p.tier === 'primary' ? (
              <DenseTag variant="success">primary</DenseTag>
            ) : (
              <DenseTag variant="category">aux</DenseTag>
            )}
            <span className="text-[var(--muted-foreground)]">{p.purpose}</span>
          </li>
        ))}
      </ul>

      <OpsSubsectionTitle>Phases</OpsSubsectionTitle>
      <ol className="m-0 list-none space-y-3 p-0">
        {STG_RELEASE_PHASES.map(phase => (
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
