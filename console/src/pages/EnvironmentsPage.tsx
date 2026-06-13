import { useCallback, useState, type ReactNode } from 'react'
import type { OpsContextResponse } from '@/api/types'
import { fetchContext } from '@/api/platform'
import {
  AUTHORIZATION_LEVELS,
  CATALOG_SOURCE,
  CATALOG_VERSION,
  FLOW_ROWS,
  HARDWARE_ROWS,
  PLATFORM_PHASES,
  SCOPE_ROWS,
  TRADE_ENVIRONMENTS,
  buildEnvironmentsLlmContext,
} from '@/lib/environments-catalog'

type CopyState = 'idle' | 'copied' | 'error'

export function EnvironmentsPage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    let spine = context
    if (spine == null) {
      try {
        spine = await fetchContext()
      } catch {
        /* static catalog only */
      }
    }
    const text = buildEnvironmentsLlmContext(spine)
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [context])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Environments</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] max-w-2xl">
              Living catalog for Bifrost hardware, CI/CD, K3s target, and trade Dev/Prod paths.
              Maintained in{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{CATALOG_SOURCE}</code>
              {' '}(v{CATALOG_VERSION}).
            </p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy for LLM'}
          </button>
        </div>
      </section>

      <CatalogSection title="Registered trade environments">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Env</th>
              <th>Nginx</th>
              <th>PostgreSQL</th>
              <th>Redis</th>
              <th>Host</th>
            </tr>
          </thead>
          <tbody>
            {TRADE_ENVIRONMENTS.map(row => (
              <tr key={row.id}>
                <td>
                  <span className={`badge-ui badge-env-${row.id}`}>{row.id}</span>
                </td>
                <td className="font-mono-tabular">{row.nginx}</td>
                <td className="font-mono-tabular">{row.postgres}</td>
                <td className="font-mono-tabular">{row.redis}</td>
                <td>{row.host}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="Scope — stack components">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Tag</th>
              <th>Component</th>
              <th>Technology</th>
              <th>Notes</th>
            </tr>
          </thead>
          <tbody>
            {SCOPE_ROWS.map(row => (
              <tr key={row.tag}>
                <td>
                  <span className="badge-ui font-mono-tabular">{row.tag}</span>
                </td>
                <td className="font-medium">{row.component}</td>
                <td>{row.technology}</td>
                <td className="text-[var(--muted-foreground)]">{row.notes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="End-to-end flow">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Path</th>
              <th>Stage</th>
              <th>Trigger</th>
              <th>Runtime</th>
              <th>Data</th>
            </tr>
          </thead>
          <tbody>
            {FLOW_ROWS.map((row, i) => (
              <tr key={`${row.path}-${row.stage}-${i}`}>
                <td>{row.path}</td>
                <td>{row.stage}</td>
                <td>{row.trigger}</td>
                <td>{row.runtime}</td>
                <td className="text-[var(--muted-foreground)]">{row.dataStore}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="Hardware nodes">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Node</th>
              <th>Host</th>
              <th>Compose role</th>
              <th>K3s role (target)</th>
            </tr>
          </thead>
          <tbody>
            {HARDWARE_ROWS.map(row => (
              <tr key={row.id}>
                <td className="font-mono-tabular">{row.id}</td>
                <td className="font-mono-tabular">{row.host}</td>
                <td>{row.roleCompose}</td>
                <td className="text-[var(--muted-foreground)]">{row.roleK3s}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Platform phases (Goal)">
          <ul className="m-0 pl-4 flex flex-col gap-2 text-[var(--text-dense)]">
            {PLATFORM_PHASES.map(p => (
              <li key={p.id}>
                <strong>Phase {p.id}</strong> — {p.label}{' '}
                <span className="text-[var(--muted-foreground)]">({p.timeframe})</span>
                <br />
                <span className="text-[var(--muted-foreground)] text-[var(--text-dense-meta)]">
                  {p.deliverables}
                </span>
              </li>
            ))}
          </ul>
        </CatalogSection>

        <CatalogSection title="Platform authorization levels">
          <ul className="m-0 pl-4 flex flex-col gap-2">
            {AUTHORIZATION_LEVELS.map(a => (
              <li key={a.level}>
                <code className="font-mono-tabular">{a.level}</code> — {a.behavior}
              </li>
            ))}
          </ul>
        </CatalogSection>
      </div>
    </div>
  )
}

function CatalogSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="page-section panel-elevated overflow-hidden">
      <header className="px-3 py-2 border-b border-[var(--border)]">
        <h3 className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
          {title}
        </h3>
      </header>
      <div className="dense-table-scroll p-0">{children}</div>
    </section>
  )
}
