import { useCallback, useState } from 'react'
import type { OpsContextResponse } from '@/api/types'
import { fetchContext } from '@/api/platform'
import { CatalogSection } from '@/components/CatalogSection'
import {
  ACTUATION_PHASES,
  AI_MERGE_RATIONALE,
  AI_PLATFORM_BOUNDARIES,
  AI_PLATFORM_CAPABILITIES,
  AI_PLATFORM_MISSION,
  AI_PLATFORM_PHASES,
  AI_PLATFORM_SUCCESS,
  BLUEPRINT_AUTHORIZATION_LEVELS,
  BLUEPRINT_SOURCE,
  BLUEPRINT_VERSION,
  CONFIG_FILES,
  CONSOLE_VIEWS,
  DESIGN_PRINCIPLES,
  NORTH_STAR_DECISION,
  NORTH_STAR_STATEMENT,
  NORTH_STAR_STRATEGY,
  OWNER_EXCEPTIONS,
  PLATFORM_API_ENDPOINTS,
  STRATEGY_C_LAYERS,
  SUCCESS_CRITERIA,
  buildBlueprintLlmPack,
} from '@/lib/architecture/blueprintCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function BlueprintPage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    let spine = context
    if (spine == null) {
      try { spine = await fetchContext() } catch { /* static only */ }
    }
    const text = buildBlueprintLlmPack(spine)
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
      {/* Page header */}
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Blueprint</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] max-w-2xl">
              North Star, system architecture, control-plane layers, and design principles.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{BLUEPRINT_SOURCE}</code>
              {' '}(v{BLUEPRINT_VERSION}).
            </p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      {/* 1 — North Star */}
      <CatalogSection title="North Star">
        <div className="px-3 py-3 text-[var(--text-dense)] flex flex-col gap-3">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="badge-ui badge-status-signed">{NORTH_STAR_STRATEGY}</span>
            <span className="text-[var(--muted-foreground)] text-xs">Decision {NORTH_STAR_DECISION}</span>
          </div>
          <p className="m-0 leading-relaxed">{NORTH_STAR_STATEMENT}</p>
        </div>
      </CatalogSection>

      {/* 2 — Owner exceptions */}
      <CatalogSection title="Owner exceptions">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Allowed (Owner-only)</th>
              <th>Forbidden (must use Console/API)</th>
            </tr>
          </thead>
          <tbody>
            {OWNER_EXCEPTIONS.map((e, i) => (
              <tr key={i}>
                <td>{e.allowed}</td>
                <td className="text-[var(--muted-foreground)]">{e.forbidden}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 3 — Strategy C layers */}
      <CatalogSection title="Strategy C — control-plane layers">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Layer</th>
              <th>Responsibility</th>
            </tr>
          </thead>
          <tbody>
            {STRATEGY_C_LAYERS.map(l => (
              <tr key={l.layer}>
                <td className="font-medium whitespace-nowrap">{l.layer}</td>
                <td>{l.responsibility}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 4 — Design principles */}
      <CatalogSection title="Design principles">
        <ul className="m-0 pl-4 py-2 flex flex-col gap-2 text-[var(--text-dense)]">
          {DESIGN_PRINCIPLES.map(p => (
            <li key={p.id}>
              <strong>{p.id}. {p.title}</strong>
              <span className="text-[var(--muted-foreground)]"> — {p.description}</span>
            </li>
          ))}
        </ul>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 5 — Console views */}
        <CatalogSection title="Console views">
          <table className="dense-table">
            <thead>
              <tr>
                <th>View</th>
                <th>Plane</th>
                <th>Purpose</th>
              </tr>
            </thead>
            <tbody>
              {CONSOLE_VIEWS.map(v => (
                <tr key={v.view}>
                  <td className="font-medium whitespace-nowrap">{v.view}</td>
                  <td><span className="badge-ui">{v.plane}</span></td>
                  <td className="text-[var(--muted-foreground)]">{v.purpose}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>

        {/* 6 — Authorization levels */}
        <CatalogSection title="Authorization levels">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Level</th>
                <th>Behavior</th>
              </tr>
            </thead>
            <tbody>
              {BLUEPRINT_AUTHORIZATION_LEVELS.map(a => (
                <tr key={a.level}>
                  <td>
                    <code className="font-mono-tabular">{a.level}</code>
                  </td>
                  <td>{a.behavior}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>
      </div>

      {/* 7 — Platform API endpoints */}
      <CatalogSection title="Platform API endpoints">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Method</th>
              <th>Path</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {PLATFORM_API_ENDPOINTS.map(e => (
              <tr key={`${e.method}-${e.path}`}>
                <td><span className="badge-ui font-mono-tabular">{e.method}</span></td>
                <td className="font-mono-tabular">{e.path}</td>
                <td className="text-[var(--muted-foreground)]">{e.description}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 8 — Configuration files */}
      <CatalogSection title="Configuration files">
        <table className="dense-table">
          <thead>
            <tr>
              <th>File</th>
              <th>Role</th>
            </tr>
          </thead>
          <tbody>
            {CONFIG_FILES.map(c => (
              <tr key={c.file}>
                <td className="font-mono-tabular">{c.file}</td>
                <td className="text-[var(--muted-foreground)]">{c.role}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 9 — Success criteria */}
      <CatalogSection title="Success criteria (north star completion)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Area</th>
              <th>Criterion</th>
            </tr>
          </thead>
          <tbody>
            {SUCCESS_CRITERIA.map(s => (
              <tr key={s.area}>
                <td className="font-medium whitespace-nowrap">{s.area}</td>
                <td>{s.criterion}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 10 — Actuation phases */}
      <CatalogSection title="Actuation phases (P0–P5)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Phase</th>
              <th>Deliverables</th>
              <th>Eliminates</th>
            </tr>
          </thead>
          <tbody>
            {ACTUATION_PHASES.map(p => (
              <tr key={p.phase}>
                <td className="font-medium whitespace-nowrap">{p.phase}</td>
                <td>{p.deliverables}</td>
                <td className="text-[var(--muted-foreground)]">{p.eliminates}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 11 — AI Native Ops Platform */}
      <CatalogSection title="AI Native Ops Platform — Mission">
        <div className="flex flex-col gap-2 px-3 py-3 text-[var(--text-dense)]">
          <p className="m-0 leading-relaxed">{AI_PLATFORM_MISSION}</p>
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{AI_MERGE_RATIONALE}</p>
        </div>
      </CatalogSection>

      {/* 12 — AI Capabilities */}
      <CatalogSection title="AI Platform capabilities">
        {AI_PLATFORM_CAPABILITIES.map(cap => (
          <div key={cap.name} className="border-b border-[var(--border)] last:border-b-0">
            <div className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">
              {cap.name}
            </div>
            <p className="m-0 px-3 py-1 text-[var(--text-dense)] text-[var(--muted-foreground)]">{cap.description}</p>
            <ul className="m-0 list-disc px-4 py-2 text-[var(--text-dense)]">
              {cap.examples.map(e => (
                <li key={e}>{e}</li>
              ))}
            </ul>
          </div>
        ))}
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 13 — AI Platform phases */}
        <CatalogSection title="AI Platform phases">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Phase</th>
                <th>Time</th>
                <th>Deliverables</th>
                <th>Business unlock</th>
              </tr>
            </thead>
            <tbody>
              {AI_PLATFORM_PHASES.map(p => (
                <tr key={p.id}>
                  <td className="font-medium whitespace-nowrap">{p.id}</td>
                  <td className="text-[var(--text-dense-meta)]">{p.timeBox}</td>
                  <td>{p.deliverables}</td>
                  <td className="text-[var(--muted-foreground)]">{p.businessUnlock}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>

        {/* 14 — AI Boundaries */}
        <CatalogSection title="AI Platform boundaries">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Rule</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {AI_PLATFORM_BOUNDARIES.map(b => (
                <tr key={b.rule}>
                  <td className="font-medium whitespace-nowrap">{b.rule}</td>
                  <td className="text-[var(--muted-foreground)]">{b.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>
      </div>

      {/* 15 — AI Platform success criteria */}
      <CatalogSection title="AI Platform success criteria">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Area</th>
              <th>Criterion</th>
            </tr>
          </thead>
          <tbody>
            {AI_PLATFORM_SUCCESS.map(s => (
              <tr key={s.area}>
                <td className="font-medium whitespace-nowrap">{s.area}</td>
                <td>{s.criterion}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>
    </div>
  )
}
