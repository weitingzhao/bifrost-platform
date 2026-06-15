import type { ReactNode } from 'react'
import type { OpsContextResponse } from '@/api/types'
import { milestoneStatusClass } from '@/components/FocusStrip'

interface ProgramPageProps {
  context: OpsContextResponse | undefined
  isLoading: boolean
  error: Error | null
  onOpenBlueprint?: () => void
}

export function ProgramPage({ context, isLoading, error, onOpenBlueprint }: ProgramPageProps) {
  if (isLoading) {
    return <p className="text-[var(--muted-foreground)]">Loading program context…</p>
  }
  if (error) {
    return <p className="lamp-fail">Failed to load context: {error.message}</p>
  }
  if (!context) return null

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <h2 className="m-0 text-sm font-semibold">Program — milestones & decisions</h2>
        <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Authoritative spine from{' '}
          <code className="font-mono-tabular">config/ops-context.yaml</code> (v
          {context.meta.version}).
        </p>
      </section>

      {context.north_star != null && (
        <ProgramSection title="North star — ultimate goal (Strategy C)">
          <div className="flex flex-col gap-3 p-4 text-[var(--text-dense)]">
            <p className="m-0 font-medium leading-snug">{context.north_star.statement}</p>
            <div className="flex flex-wrap gap-2">
              <span className="badge-ui font-mono-tabular">{context.north_star.id}</span>
              <span className="badge-ui badge-status-progress font-mono-tabular">
                {context.north_star.strategy}
              </span>
            </div>
            <div>
              <h4 className="m-0 mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
                Owner exception
              </h4>
              <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                {context.north_star.owner_exception}
              </p>
            </div>
            <div>
              <h4 className="m-0 mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
                Principles
              </h4>
              <ul className="m-0 list-disc space-y-1 pl-5 text-[var(--text-dense-meta)]">
                {context.north_star.principles.map(p => (
                  <li key={p}>{p}</li>
                ))}
              </ul>
            </div>
            <div>
              <h4 className="m-0 mb-1 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
                Success criteria
              </h4>
              <ul className="m-0 list-disc space-y-1 pl-5 text-[var(--text-dense-meta)]">
                {context.north_star.success_criteria.map(c => (
                  <li key={c}>{c}</li>
                ))}
              </ul>
            </div>
            <p className="m-0 font-mono-tabular text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {context.north_star.authority}
            </p>
            {onOpenBlueprint != null && (
              <div className="flex flex-wrap gap-2 pt-1">
                <button type="button" className="btn-ui btn-ui-ghost text-xs" onClick={onOpenBlueprint}>
                  Open Blueprint
                </button>
              </div>
            )}
          </div>
        </ProgramSection>
      )}

      <ProgramSection title="Milestones">
        <table className="dense-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Label</th>
              <th>Status</th>
              <th>Blocker</th>
              <th>Signed</th>
            </tr>
          </thead>
          <tbody>
            {context.milestones.map(m => (
              <tr key={m.id}>
                <td className="font-mono-tabular">{m.id}</td>
                <td>{m.label ?? '—'}</td>
                <td>
                  <span className={milestoneStatusClass(m.status)}>{m.status}</span>
                </td>
                <td className="font-mono-tabular text-[var(--muted-foreground)]">
                  {m.blocker ?? '—'}
                </td>
                <td className="font-mono-tabular">{m.signed_at ?? '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </ProgramSection>

      <ProgramSection title="Owner decisions">
        <div className="grid gap-3 p-3 md:grid-cols-2">
          {context.decisions.map(d => (
            <article
              key={d.id}
              className="rounded-md border border-[var(--border)] bg-[var(--card)] p-3"
            >
              <div className="flex flex-wrap items-center gap-2">
                <code className="font-mono-tabular text-sm font-semibold">{d.id}</code>
                <span className={milestoneStatusClass(d.status)}>{d.status}</span>
                {d.signed_at != null && (
                  <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                    {d.signed_at}
                  </span>
                )}
              </div>
              {d.topic != null && d.topic !== '' && (
                <p className="m-0 mt-2 text-xs font-medium text-[var(--muted-foreground)]">
                  {d.topic}
                </p>
              )}
              <p className="m-0 mt-2 text-[var(--text-dense)]">{d.conclusion}</p>
              {d.authority != null && (
                <p className="m-0 mt-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] font-mono-tabular">
                  {d.authority}
                </p>
              )}
            </article>
          ))}
        </div>
      </ProgramSection>

      <ProgramSection title="Platform roadmap phases (Goal)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Phase</th>
              <th>Label</th>
              <th>Timeframe</th>
              <th>Deliverables</th>
            </tr>
          </thead>
          <tbody>
            {context.platform_phases.map(p => (
              <tr key={p.id}>
                <td className="font-mono-tabular">{p.id}</td>
                <td>{p.label}</td>
                <td>{p.timeframe}</td>
                <td className="text-[var(--muted-foreground)]">{p.deliverables}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </ProgramSection>

      <ProgramSection title="Coupling surfaces (flywheel boundary)">
        <ul className="m-0 list-disc px-5 py-3 text-[var(--text-dense)]">
          {context.coupling_surfaces.map(s => (
            <li key={s}>
              <code className="font-mono-tabular">{s}</code>
            </li>
          ))}
        </ul>
      </ProgramSection>
    </div>
  )
}

function ProgramSection({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="border-b border-[var(--border)] px-3 py-2">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          {title}
        </h3>
      </header>
      <div className="dense-table-scroll p-0">{children}</div>
    </section>
  )
}
