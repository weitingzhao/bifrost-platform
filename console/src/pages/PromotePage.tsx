import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { milestoneStatusVariant } from '@/components/FocusStrip'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'
import { OpsSection } from '@/components/layout/OpsSection'

const FLYWHEEL_A_CHECKS = [
  'npm run lint',
  'npm run build',
  'npm run check:legacy-css',
  'Page-by-page Legacy equivalence (Phase 1)',
] as const

const FLYWHEEL_B_CHECKS = [
  'make prod-health (12/12)',
  'scripts/release_gate.sh (when available)',
  'Platform GET /api/v1/matrix?env=prod',
  'Owner sign-off chain',
] as const

interface PromotePageProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  isLoading: boolean
  onOpenProgram: () => void
  onOpenDelivery?: () => void
}

export function PromotePage({
  context,
  matrices,
  isLoading,
  onOpenProgram,
  onOpenDelivery,
}: PromotePageProps) {
  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading promotion context…</p>
  }

  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const promote = evaluatePromoteStatus(context, matrices)
  const { ready, blockedByDecision, prodFails, gateDone } = promote
  const gate = context.promotion.last_gate

  const staging = context.environments_extended.staging

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Release readiness"
        actions={
          <DenseTag variant={ready ? 'success' : 'danger'}>
            {ready ? 'Ready (narrative)' : 'Blocked'}
          </DenseTag>
        }
        description={
          <>
            Read-only checklist for flywheel A + B promotion. CI/CD path diagram lives on{' '}
            {onOpenDelivery != null ? (
              <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
                Delivery
              </button>
            ) : (
              <strong>Delivery</strong>
            )}
            . No write actions at L0.
          </>
        }
        overflow="visible"
      />

      {!ready && (
        <section className="page-section panel-elevated px-4 py-3 lamp-warn">
          <p className="m-0 text-[var(--text-dense)]">
            Production cutover is not ready.
            {blockedByDecision && cutover?.blocker != null && (
              <>
                {' '}
                Milestone <code className="font-mono-tabular">{cutover.id}</code> blocked on{' '}
                <button type="button" className="focus-strip-link" onClick={onOpenProgram}>
                  {cutover.blocker}
                </button>
                .
              </>
            )}
            {prodFails && <> Prod matrix has failing targets.</>}
            {!gateDone && <> Release gate has not been recorded yet.</>}
          </p>
        </section>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <PromoteSection title="Flywheel A — Trade frontend (Now)">
          <ul className="m-0 list-disc px-5 py-3 text-[var(--text-dense)]">
            {FLYWHEEL_A_CHECKS.map(c => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </PromoteSection>

        <PromoteSection title="Flywheel B — Runtime & ops (Now)">
          <ul className="m-0 list-disc px-5 py-3 text-[var(--text-dense)]">
            {FLYWHEEL_B_CHECKS.map(c => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </PromoteSection>
      </div>

      <PromoteSection title="Release gate (spine)">
        <DenseDataTable>
          <DenseTableBody>
            <DenseTableRow>
              <DenseTableHead className="text-left">Last run</DenseTableHead>
              <DenseTableCell className="font-mono-tabular">{gate.at ?? '—'}</DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableHead className="text-left">Result</DenseTableHead>
              <DenseTableCell>
                {gate.result != null ? (
                  <DenseTag variant={milestoneStatusVariant(gate.result === 'pass' ? 'SIGNED' : 'BLOCKED_ON')}>
                    {gate.result}
                  </DenseTag>
                ) : (
                  '—'
                )}
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableHead className="text-left">Log path</DenseTableHead>
              <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">{gate.log_path}</DenseTableCell>
            </DenseTableRow>
          </DenseTableBody>
        </DenseDataTable>
        {gate.result == null && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] lamp-warn">
            No gate recorded — run release_gate.sh when available (Phase A).
          </p>
        )}
      </PromoteSection>

      {staging != null && (
        <PromoteSection title="Staging environment">
          <p className="m-0 px-3 py-2 text-[var(--text-dense)]">
            Status:{' '}
            <DenseTag variant={milestoneStatusVariant(staging.status)}>{staging.status}</DenseTag>
            {staging.note != null && (
              <span className="text-[var(--muted-foreground)]"> — {staging.note}</span>
            )}
          </p>
        </PromoteSection>
      )}

      {context.probe_hints.length > 0 && (
        <PromoteSection title="Probe hints — related Trade routes">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Target</DenseTableHead>
                <DenseTableHead>Trade route</DenseTableHead>
                <DenseTableHead>Hint</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {context.probe_hints.map(h => (
                <DenseTableRow key={h.target_id}>
                  <DenseTableCell className="font-mono-tabular">{h.target_id}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{h.trade_route}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{h.hint}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </PromoteSection>
      )}
    </div>
  )
}

function PromoteSection({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <OpsSection title={title} bodyPadding="none" overflow="hidden">
      {children}
    </OpsSection>
  )
}
