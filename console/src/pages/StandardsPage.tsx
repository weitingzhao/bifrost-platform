import { useCallback, useState } from 'react'
import { CatalogSection } from '@/components/CatalogSection'
import {
  ACTUATION_API_ROUTES,
  ACTUATION_PHASE_MATRIX,
  AUTH_PROBES,
  HTTP_PROBES,
  OBSERVABILITY_LAYERS,
  POLICY_BLOCKED,
  STANDARDS_SOURCE,
  STANDARDS_VERSION,
  TCP_PROBES,
  buildStandardsLlmPack,
} from '@/lib/architecture/standardsCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function StandardsPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    const text = buildStandardsLlmPack()
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      {/* Page header */}
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Standards</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] max-w-2xl">
              Trade stack probe contract, cluster actuation phases, and API route inventory.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{STANDARDS_SOURCE}</code>
              {' '}(v{STANDARDS_VERSION}).
            </p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      {/* 1 — HTTP probes */}
      <CatalogSection title="HTTP probes (via nginx)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Target ID</th>
              <th>Path</th>
              <th>OK codes</th>
            </tr>
          </thead>
          <tbody>
            {HTTP_PROBES.map(p => (
              <tr key={p.targetId}>
                <td className="font-mono-tabular">{p.targetId}</td>
                <td className="font-mono-tabular">{p.path}</td>
                <td>{p.okCodes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 2 — Auth probe */}
        <CatalogSection title="Auth probe">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Target ID</th>
                <th>Path</th>
                <th>Token</th>
              </tr>
            </thead>
            <tbody>
              {AUTH_PROBES.map(p => (
                <tr key={p.targetId}>
                  <td className="font-mono-tabular">{p.targetId}</td>
                  <td className="font-mono-tabular">{p.path}</td>
                  <td className="text-[var(--muted-foreground)]">{p.token}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>

        {/* 3 — TCP probes */}
        <CatalogSection title="TCP probes">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Target ID</th>
                <th>Address source</th>
              </tr>
            </thead>
            <tbody>
              {TCP_PROBES.map(p => (
                <tr key={p.targetId}>
                  <td className="font-mono-tabular">{p.targetId}</td>
                  <td>{p.addressSource}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>
      </div>

      {/* 4 — Policy-blocked */}
      <CatalogSection title="Policy-blocked rows">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Target ID</th>
              <th>Reason</th>
            </tr>
          </thead>
          <tbody>
            {POLICY_BLOCKED.map(p => (
              <tr key={p.targetId}>
                <td className="font-mono-tabular">{p.targetId}</td>
                <td className="text-[var(--muted-foreground)]">{p.reason}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 5 — Actuation phase matrix */}
      <CatalogSection title="Cluster actuation phase matrix">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Phase</th>
              <th>Nodes</th>
              <th>Workloads</th>
              <th>GitOps</th>
              <th>Stack</th>
              <th>Audit</th>
            </tr>
          </thead>
          <tbody>
            {ACTUATION_PHASE_MATRIX.map(p => (
              <tr key={p.phase}>
                <td className="font-medium whitespace-nowrap">
                  <span className="badge-ui">{p.phase}</span>
                </td>
                <td>{p.nodes}</td>
                <td>{p.workloads}</td>
                <td>{p.gitops}</td>
                <td>{p.stack}</td>
                <td className="text-[var(--muted-foreground)]">{p.audit}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 6 — Actuation API routes */}
      <CatalogSection title="Actuation API routes (P1–P4)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Phase</th>
              <th>Method</th>
              <th>Route</th>
              <th>Role</th>
              <th>Purpose</th>
            </tr>
          </thead>
          <tbody>
            {ACTUATION_API_ROUTES.map((r, i) => (
              <tr key={i}>
                <td><span className="badge-ui">{r.phase}</span></td>
                <td><span className="badge-ui font-mono-tabular">{r.method}</span></td>
                <td className="font-mono-tabular">{r.route}</td>
                <td>{r.role}</td>
                <td className="text-[var(--muted-foreground)]">{r.purpose}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 7 — Observability layers */}
      <CatalogSection title="Observability layers (A vs B)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Layer</th>
              <th>Scope</th>
              <th>Data source</th>
              <th>Notes</th>
            </tr>
          </thead>
          <tbody>
            {OBSERVABILITY_LAYERS.map(l => (
              <tr key={l.layer}>
                <td className="font-medium whitespace-nowrap">{l.layer}</td>
                <td>{l.scope}</td>
                <td>{l.dataSource}</td>
                <td className="text-[var(--muted-foreground)]">{l.notes}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>
    </div>
  )
}
