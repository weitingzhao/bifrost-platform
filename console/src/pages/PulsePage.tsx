import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { MatrixTable } from '@/components/MatrixTable'
import { flywheelLabel, milestoneStatusClass } from '@/components/FocusStrip'
import { StatusLamp } from '@bifrost/ui'

interface PulsePageProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  matrixError: Error | null
  platformHealthy: boolean
  onOpenMatrix: () => void
  onOpenProgram: () => void
}

function countReach(matrix: MatrixResponse): { ok: number; fail: number; total: number } {
  let ok = 0
  let fail = 0
  for (const t of matrix.targets) {
    if (t.reachability === 'ok' || t.reachability === 'degraded') ok += 1
    else if (t.reachability === 'fail') fail += 1
  }
  return { ok, fail, total: matrix.targets.length }
}

export function PulsePage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  matrixError,
  platformHealthy,
  onOpenMatrix,
  onOpenProgram,
}: PulsePageProps) {
  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <h2 className="m-0 text-sm font-semibold">Pulse — live & program snapshot</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Flywheel B runtime health plus spine focus. Refreshes with matrix probes (~30s).
        </p>
      </section>

      <div className="grid gap-4 md:grid-cols-3">
        <PulseCard title="Ops API">
          <StatusLamp value={platformHealthy ? 'ok' : 'fail'} kind="reach" />
          <span className="ml-2 text-[var(--text-dense)]">
            platform-api {platformHealthy ? 'healthy' : 'down'}
          </span>
        </PulseCard>

        <PulseCard title="Deployment">
          {contextLoading || !context ? (
            <span className="text-[var(--muted-foreground)]">…</span>
          ) : (
            <>
              <code className="font-mono-tabular">{context.deployment.phase}</code>
              <span className="text-[var(--muted-foreground)]"> · </span>
              <code className="font-mono-tabular">{context.deployment.active_track}</code>
            </>
          )}
        </PulseCard>

        <PulseCard title="Primary flywheel">
          {contextLoading || !context ? (
            <span className="text-[var(--muted-foreground)]">…</span>
          ) : (
            <span>{flywheelLabel(context.focus.flywheel_primary)}</span>
          )}
        </PulseCard>
      </div>

      {context != null && context.focus.blocker != null && context.focus.blocker !== '' && (
        <section className="page-section panel-elevated px-4 py-3 lamp-warn">
          <strong>Focus blocker:</strong> {context.focus.headline} —{' '}
          <button type="button" className="focus-strip-link" onClick={onOpenProgram}>
            {context.focus.blocker}
          </button>
        </section>
      )}

      <section className="page-section panel-elevated overflow-hidden">
        <header className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--border)] px-3 py-2">
          <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
            Environment reachability
          </h3>
          <button type="button" className="btn-ui btn-ui-ghost text-xs" onClick={onOpenMatrix}>
            Open full matrix
          </button>
        </header>
        {matrixLoading && (
          <p className="px-3 py-2 text-[var(--muted-foreground)]">Probing targets…</p>
        )}
        {matrixError != null && (
          <p className="lamp-fail px-3 py-2">Failed to load matrix: {matrixError.message}</p>
        )}
        {!matrixLoading && matrices.length > 0 && (
          <table className="dense-table">
            <thead>
              <tr>
                <th>Env</th>
                <th>OK</th>
                <th>Fail</th>
                <th>Total</th>
                <th>Probed at</th>
              </tr>
            </thead>
            <tbody>
              {matrices.map(m => {
                const c = countReach(m)
                return (
                  <tr key={m.environment}>
                    <td>
                      <span className={`badge-ui badge-env-${m.environment}`}>
                        {m.environment}
                      </span>
                    </td>
                    <td className="font-mono-tabular lamp-ok">{c.ok}</td>
                    <td className={`font-mono-tabular ${c.fail > 0 ? 'lamp-fail' : ''}`}>
                      {c.fail}
                    </td>
                    <td className="font-mono-tabular">{c.total}</td>
                    <td className="font-mono-tabular text-[var(--muted-foreground)]">
                      {m.generated_at}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </section>

      {context != null && (
        <section className="page-section panel-elevated overflow-hidden">
          <header className="border-b border-[var(--border)] px-3 py-2">
            <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              Active milestones (summary)
            </h3>
          </header>
          <table className="dense-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>Status</th>
                <th>Blocker</th>
              </tr>
            </thead>
            <tbody>
              {context.milestones
                .filter(m => m.status !== 'CLOSED')
                .map(m => (
                  <tr key={m.id}>
                    <td className="font-mono-tabular">{m.id}</td>
                    <td>
                      <span className={milestoneStatusClass(m.status)}>{m.status}</span>
                    </td>
                    <td className="font-mono-tabular text-[var(--muted-foreground)]">
                      {m.blocker ?? '—'}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
          <div className="border-t border-[var(--border)] px-3 py-2">
            <button type="button" className="btn-ui btn-ui-ghost text-xs" onClick={onOpenProgram}>
              Full program & decisions
            </button>
          </div>
        </section>
      )}

      {matrices.length > 0 && (
        <details className="page-section panel-elevated">
          <summary className="cursor-pointer px-4 py-2 text-sm font-medium">
            Expand per-target matrix
          </summary>
          <div className="flex flex-col gap-4 p-3">
            {matrices.map(m => (
              <MatrixTable key={m.environment} matrix={m} />
            ))}
          </div>
        </details>
      )}
    </div>
  )
}

function PulseCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="page-section panel-elevated px-4 py-3">
      <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
        {title}
      </h3>
      <div className="mt-2 flex items-center text-[var(--text-dense)]">{children}</div>
    </section>
  )
}
