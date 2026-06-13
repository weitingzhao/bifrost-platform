import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { milestoneStatusClass } from '@/components/FocusStrip'
import { evaluatePromoteStatus } from '@/lib/control-room/matrixSummary'

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
}

export function PromotePage({
  context,
  matrices,
  isLoading,
  onOpenProgram,
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
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-center gap-3">
          <h2 className="m-0 text-sm font-semibold">Promote — release readiness (L0)</h2>
          <span className={ready ? 'badge-ui badge-status-signed' : 'badge-ui badge-status-blocked'}>
            {ready ? 'Ready (narrative)' : 'Blocked'}
          </span>
        </div>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Read-only checklist for flywheel A + B promotion. No write actions at L0.
        </p>
      </section>

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
        <PromoteSection title="Flywheel A — Trade frontend">
          <ul className="m-0 list-disc px-5 py-3 text-[var(--text-dense)]">
            {FLYWHEEL_A_CHECKS.map(c => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </PromoteSection>

        <PromoteSection title="Flywheel B — Runtime & ops">
          <ul className="m-0 list-disc px-5 py-3 text-[var(--text-dense)]">
            {FLYWHEEL_B_CHECKS.map(c => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </PromoteSection>
      </div>

      <PromoteSection title="Release gate (spine)">
        <table className="dense-table">
          <tbody>
            <tr>
              <th className="text-left">Last run</th>
              <td className="font-mono-tabular">{gate.at ?? '—'}</td>
            </tr>
            <tr>
              <th className="text-left">Result</th>
              <td>
                {gate.result != null ? (
                  <span className={milestoneStatusClass(gate.result === 'pass' ? 'SIGNED' : 'BLOCKED_ON')}>
                    {gate.result}
                  </span>
                ) : (
                  '—'
                )}
              </td>
            </tr>
            <tr>
              <th className="text-left">Log path</th>
              <td className="font-mono-tabular text-[var(--muted-foreground)]">{gate.log_path}</td>
            </tr>
          </tbody>
        </table>
      </PromoteSection>

      {staging != null && (
        <PromoteSection title="Staging environment">
          <p className="m-0 px-3 py-2 text-[var(--text-dense)]">
            Status:{' '}
            <span className={milestoneStatusClass(staging.status)}>{staging.status}</span>
            {staging.note != null && (
              <span className="text-[var(--muted-foreground)]"> — {staging.note}</span>
            )}
          </p>
        </PromoteSection>
      )}

      {context.probe_hints.length > 0 && (
        <PromoteSection title="Probe hints — related Trade routes">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Target</th>
                <th>Trade route</th>
                <th>Hint</th>
              </tr>
            </thead>
            <tbody>
              {context.probe_hints.map(h => (
                <tr key={h.target_id}>
                  <td className="font-mono-tabular">{h.target_id}</td>
                  <td className="font-mono-tabular">{h.trade_route}</td>
                  <td className="text-[var(--muted-foreground)]">{h.hint}</td>
                </tr>
              ))}
            </tbody>
          </table>
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
    <section className="page-section panel-elevated overflow-hidden">
      <header className="border-b border-[var(--border)] px-3 py-2">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          {title}
        </h3>
      </header>
      {children}
    </section>
  )
}
