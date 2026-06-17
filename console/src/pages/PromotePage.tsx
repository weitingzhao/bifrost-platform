import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { MatrixResponse, OpsContextResponse, ReleaseGateResponse } from '@/api/types'
import { runReleaseGate } from '@/api/platform'
import { milestoneStatusVariant } from '@/components/FocusStrip'
import { OpsSection } from '@/components/layout/OpsSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'

const FLYWHEEL_A_CHECKS = [
  'npm run lint',
  'npm run build',
  'npm run check:legacy-css',
  'Page-by-page Legacy equivalence (Phase 1)',
] as const

const FLYWHEEL_B_CHECKS = [
  'make prod-health (12/12)',
  'POST /api/v1/promote/release-gate (admin token)',
  'Platform GET /api/v1/matrix?env=prod',
  'K3s stg smoke (api-monitor NodePort)',
  'Owner sign-off chain',
] as const

interface PromotePageProps {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  releaseGate?: ReleaseGateResponse
  releaseGateLoading?: boolean
  releaseGateError?: string | null
  isLoading: boolean
  onOpenProgram: () => void
  onOpenDelivery?: () => void
}

export function PromotePage({
  context,
  matrices,
  releaseGate,
  releaseGateLoading = false,
  releaseGateError = null,
  isLoading,
  onOpenProgram,
  onOpenDelivery,
}: PromotePageProps) {
  const { canAdmin } = usePlatformAuth()
  const qc = useQueryClient()
  const [runError, setRunError] = useState<string | null>(null)

  const runMutation = useMutation({
    mutationFn: runReleaseGate,
    onMutate: () => setRunError(null),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['promote', 'release-gate'] })
      void qc.invalidateQueries({ queryKey: ['context'] })
      void qc.invalidateQueries({ queryKey: ['platform', 'audit'] })
    },
    onError: (err: Error) => setRunError(err.message),
  })

  if (isLoading || !context) {
    return <p className="text-[var(--muted-foreground)]">Loading promotion context…</p>
  }

  const cutover = context.milestones.find(m => m.id === '2c-b-prod-cutover')
  const promote = evaluatePromoteStatus(context, matrices)
  const { ready, blockedByDecision, prodFails, gateDone } = promote
  const gate = context.promotion.last_gate
  const checks = releaseGate?.checks ?? []

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
            Flywheel A + B promotion checklist. Run release gate via API (admin token) — aggregates
            prod matrix, cutover milestone, and K3s stg smoke. CI/CD path on{' '}
            {onOpenDelivery != null ? (
              <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
                Delivery
              </button>
            ) : (
              <strong>Delivery</strong>
            )}
            .
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

      <OpsSection
        title="Release gate"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              POST /api/v1/promote/release-gate
            </span>
            {canAdmin && (
              <Button size="sm" disabled={runMutation.isPending} onClick={() => runMutation.mutate()}>
                {runMutation.isPending ? 'Running…' : 'Run release gate'}
              </Button>
            )}
          </div>
        }
        headerExtra={
          <>
            {releaseGateError != null && releaseGateError !== '' && (
              <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">
                {releaseGateError}
              </p>
            )}
            {runError != null && (
              <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{runError}</p>
            )}
            {!releaseGateLoading && releaseGate != null && releaseGateError == null && (
              <p className="m-0 mt-2 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)]">
                <StatusLamp value={releaseGate.reachability} kind="reach" />
                <span>{releaseGate.detail}</span>
              </p>
            )}
          </>
        }
        bodyPadding="none"
        overflow="visible"
        bodyClassName="ops-section-body--table"
      >
        <DenseDataTable>
          <DenseTableBody>
            <DenseTableRow>
              <DenseTableHead className="text-left">Last run</DenseTableHead>
              <DenseTableCell className="font-mono-tabular">
                {gate.at ?? releaseGate?.at ?? '—'}
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableHead className="text-left">Result</DenseTableHead>
              <DenseTableCell>
                {(gate.result ?? releaseGate?.result) != null &&
                (gate.result ?? releaseGate?.result) !== '' ? (
                  <DenseTag
                    variant={milestoneStatusVariant(
                      (gate.result ?? releaseGate?.result) === 'pass' ? 'SIGNED' : 'BLOCKED_ON',
                    )}
                  >
                    {gate.result ?? releaseGate?.result}
                  </DenseTag>
                ) : (
                  '—'
                )}
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableHead className="text-left">Narrative ready</DenseTableHead>
              <DenseTableCell>
                <DenseTag variant={releaseGate?.ready === true ? 'success' : 'warning'}>
                  {releaseGate?.ready === true ? 'yes' : 'no'}
                </DenseTag>
              </DenseTableCell>
            </DenseTableRow>
            <DenseTableRow>
              <DenseTableHead className="text-left">Log path</DenseTableHead>
              <DenseTableCell className="font-mono-tabular text-[var(--muted-foreground)]">
                {gate.log_path ?? releaseGate?.log_path ?? '—'}
              </DenseTableCell>
            </DenseTableRow>
          </DenseTableBody>
        </DenseDataTable>

        {checks.length > 0 && (
          <>
            <div className="border-t border-[var(--border)] px-3 py-2 text-[var(--text-dense-label)] font-medium">
              Last gate checks
            </div>
            <DenseDataTable>
              <DenseTableHeader>
                <DenseTableHeadRow>
                  <DenseTableHead>Check</DenseTableHead>
                  <DenseTableHead>Required</DenseTableHead>
                  <DenseTableHead>Reach</DenseTableHead>
                  <DenseTableHead>Detail</DenseTableHead>
                </DenseTableHeadRow>
              </DenseTableHeader>
              <DenseTableBody>
                {checks.map(c => (
                  <DenseTableRow key={c.id}>
                    <DenseTableCell className="font-medium">{c.label || c.id}</DenseTableCell>
                    <DenseTableCell>{c.required ? 'yes' : 'no'}</DenseTableCell>
                    <DenseTableCell>
                      <StatusLamp value={c.reachability} kind="reach" />
                    </DenseTableCell>
                    <DenseTableCell className="text-[var(--muted-foreground)]">{c.detail}</DenseTableCell>
                  </DenseTableRow>
                ))}
              </DenseTableBody>
            </DenseDataTable>
          </>
        )}

        {!gateDone && checks.length === 0 && (
          <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] lamp-warn">
            No gate recorded — click Run release gate (admin token required).
          </p>
        )}
      </OpsSection>

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
