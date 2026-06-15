import { useCallback, useState } from 'react'
import { CatalogSection } from '@/components/CatalogSection'
import {
  AGENT_GOVERNANCE_ASSETS,
  CSS_EXCEPTIONS,
  DESIGN_SYSTEM_SOURCE,
  DESIGN_SYSTEM_VERSION,
  FORBIDDEN_PATTERNS,
  LAYER_STACK,
  LIVING_CONTRACT_PATH,
  MANDATORY_MAPPING,
  PAGE_SURFACES,
  PRIMITIVES,
  SEMANTIC_COLORS,
  TRADE_FRONTEND_URL_DEFAULT,
  buildDesignSystemLlmPack,
} from '@/lib/standards/designSystemCatalog'

type CopyState = 'idle' | 'copied' | 'error'

const tradeFrontendUrl =
  import.meta.env.VITE_TRADE_FRONTEND_URL ?? TRADE_FRONTEND_URL_DEFAULT

export function DesignSystemPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    const text = buildDesignSystemLlmPack()
    try {
      await navigator.clipboard.writeText(text)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [])

  const taxonomies = [...new Set(SEMANTIC_COLORS.map(c => c.taxonomy))]

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      {/* Page header */}
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Dense UI — Design System</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] max-w-2xl">
              Same business interaction → same shared UI primitive. Change tokens/components once → all adopters upgrade together.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{DESIGN_SYSTEM_SOURCE}</code>
              {' '}(v{DESIGN_SYSTEM_VERSION}).
            </p>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Living visual contract:{' '}
              <a
                href={`${tradeFrontendUrl}${LIVING_CONTRACT_PATH}`}
                target="_blank"
                rel="noopener noreferrer"
                className="text-[var(--primary)] underline"
              >
                Settings → UI Design System
              </a>
              {' '}(bifrost-trade-frontend)
            </p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      {/* 1 — Layer stack */}
      <CatalogSection title="Layer stack (do not skip layers)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Layer</th>
              <th>Location</th>
              <th>Role</th>
            </tr>
          </thead>
          <tbody>
            {LAYER_STACK.map(l => (
              <tr key={l.layer}>
                <td className="font-medium whitespace-nowrap">{l.layer}</td>
                <td className="font-mono-tabular">{l.location}</td>
                <td className="text-[var(--muted-foreground)]">{l.role}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 2 — Page canvas surfaces */}
      <CatalogSection title="Page canvas (three surfaces)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Surface</th>
              <th>Tailwind</th>
              <th>Usage</th>
            </tr>
          </thead>
          <tbody>
            {PAGE_SURFACES.map(s => (
              <tr key={s.surface}>
                <td className="font-medium whitespace-nowrap">{s.surface}</td>
                <td className="font-mono-tabular">{s.tailwind}</td>
                <td className="text-[var(--muted-foreground)]">{s.usage}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 3 — Business semantic colors */}
      <CatalogSection title="Business semantic colors (three independent taxonomies)">
        <div className="flex flex-col gap-3">
          {taxonomies.map(tax => (
            <div key={tax}>
              <h4 className="m-0 mb-1 text-xs font-semibold text-[var(--muted-foreground)] uppercase tracking-wider">{tax}</h4>
              <table className="dense-table">
                <thead>
                  <tr>
                    <th>Concept</th>
                    <th>Token</th>
                    <th>Utility</th>
                    <th>Accessor</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {SEMANTIC_COLORS.filter(c => c.taxonomy === tax).map(c => (
                    <tr key={c.concept}>
                      <td className="font-medium">{c.concept}</td>
                      <td className="font-mono-tabular">{c.token}</td>
                      <td className="font-mono-tabular">{c.utility}</td>
                      <td className="text-[var(--muted-foreground)]">{c.accessor}</td>
                      <td>
                        <span className={`badge-ui ${c.status === 'live' ? 'badge-ui-success' : ''}`}>
                          {c.status}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ))}
        </div>
      </CatalogSection>

      {/* 4 — Mandatory mapping (core of the standard) */}
      <CatalogSection title="Mandatory interaction → primitive mapping">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Interaction</th>
              <th>Use</th>
              <th>Never</th>
            </tr>
          </thead>
          <tbody>
            {MANDATORY_MAPPING.map(m => (
              <tr key={m.interaction}>
                <td className="font-medium whitespace-nowrap">{m.interaction}</td>
                <td className="font-mono-tabular">{m.use}</td>
                <td className="text-[var(--destructive)]">{m.never}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 5 — Primitives inventory */}
      <CatalogSection title="Primitives inventory (src/components/data-display/)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Category</th>
              <th>Component(s)</th>
              <th>File</th>
            </tr>
          </thead>
          <tbody>
            {PRIMITIVES.map((p, i) => (
              <tr key={i}>
                <td>
                  <span className="badge-ui">{p.category}</span>
                </td>
                <td className="font-mono-tabular">{p.name}</td>
                <td className="font-mono-tabular text-[var(--muted-foreground)]">{p.file}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 6 — Forbidden patterns */}
        <CatalogSection title="Forbidden patterns">
          <ul className="m-0 list-none p-0 text-[var(--text-dense)] space-y-1">
            {FORBIDDEN_PATTERNS.map((f, i) => (
              <li key={i} className="flex items-start gap-2">
                <span className="text-[var(--destructive)] shrink-0">✕</span>
                <span>{f}</span>
              </li>
            ))}
          </ul>
        </CatalogSection>

        {/* 7 — Allowed CSS exceptions */}
        <CatalogSection title="Allowed CSS exceptions (narrow)">
          <ul className="m-0 list-none p-0 text-[var(--text-dense)] space-y-1">
            {CSS_EXCEPTIONS.map((e, i) => (
              <li key={i} className="flex items-start gap-2">
                <span className="text-[var(--primary)] shrink-0">✓</span>
                <span>{e}</span>
              </li>
            ))}
          </ul>
        </CatalogSection>
      </div>

      {/* 8 — Agent governance references */}
      <CatalogSection title="Agent governance assets (cross-repo)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Asset</th>
              <th>Repo</th>
              <th>Purpose</th>
            </tr>
          </thead>
          <tbody>
            {AGENT_GOVERNANCE_ASSETS.map(a => (
              <tr key={a.asset}>
                <td className="font-mono-tabular">{a.asset}</td>
                <td>{a.repo}</td>
                <td className="text-[var(--muted-foreground)]">{a.purpose}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>
    </div>
  )
}
