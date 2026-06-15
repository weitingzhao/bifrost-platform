import { useCallback, useState } from 'react'
import { CatalogSection } from '@/components/CatalogSection'
import {
  AGENT_MODES,
  AGENT_PROTOCOL_SOURCE,
  AGENT_PROTOCOL_VERSION,
  CONTEXT_PACK_BUTTONS,
  CONTEXT_PACK_LAYERS,
  FORBIDDEN_ACTIONS,
  MODE_SELECTION_HINTS,
  OPENING_PROMPTS,
  buildAgentProtocolLlmPack,
} from '@/lib/architecture/agentProtocolCatalog'

type CopyState = 'idle' | 'copied' | 'error'

export function AgentProtocolPage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopyForLlm = useCallback(async () => {
    const text = buildAgentProtocolLlmPack()
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
            <h2 className="m-0 text-sm font-semibold">Agent Protocol</h2>
            <p className="m-0 mt-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)] max-w-2xl">
              Agent interaction modes, context pack layers, forbidden actions, and session startup guidance.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{AGENT_PROTOCOL_SOURCE}</code>
              {' '}(v{AGENT_PROTOCOL_VERSION}).
            </p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      {/* 1 — Agent modes */}
      <CatalogSection title="Agent modes">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Mode</th>
              <th>Flywheel</th>
              <th>Default UI</th>
              <th>Agent may</th>
              <th>Agent must not</th>
            </tr>
          </thead>
          <tbody>
            {AGENT_MODES.map(m => (
              <tr key={m.mode}>
                <td className="font-medium whitespace-nowrap">
                  <span className="badge-ui badge-status-signed">{m.mode}</span>
                </td>
                <td>{m.flywheel}</td>
                <td className="font-mono-tabular text-xs">{m.defaultUI}</td>
                <td>{m.agentMay}</td>
                <td className="text-[var(--muted-foreground)]">{m.agentMustNot}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* Mode selection hints */}
      <CatalogSection title="Mode selection hints">
        <ul className="m-0 pl-4 py-2 flex flex-col gap-1 text-[var(--text-dense)]">
          {MODE_SELECTION_HINTS.map((h, i) => (
            <li key={i} className="font-mono-tabular text-xs">{h}</li>
          ))}
        </ul>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        {/* 2 — Context pack buttons */}
        <CatalogSection title="Control Room context pack buttons">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Button</th>
                <th>Contents</th>
              </tr>
            </thead>
            <tbody>
              {CONTEXT_PACK_BUTTONS.map(b => (
                <tr key={b.button}>
                  <td className="font-medium whitespace-nowrap">{b.button}</td>
                  <td className="text-[var(--muted-foreground)]">{b.contents}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>

        {/* 3 — Context pack layers */}
        <CatalogSection title="Context pack layers (session startup)">
          <table className="dense-table">
            <thead>
              <tr>
                <th>#</th>
                <th>Layer</th>
                <th>Description</th>
              </tr>
            </thead>
            <tbody>
              {CONTEXT_PACK_LAYERS.map(l => (
                <tr key={l.order}>
                  <td className="font-mono-tabular text-center">{l.order}</td>
                  <td className="font-medium whitespace-nowrap">{l.name}</td>
                  <td className="text-[var(--muted-foreground)]">{l.description}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CatalogSection>
      </div>

      {/* 4 — Forbidden actions */}
      <CatalogSection title="Forbidden actions (all modes)">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Action</th>
              <th>Scope</th>
            </tr>
          </thead>
          <tbody>
            {FORBIDDEN_ACTIONS.map((f, i) => (
              <tr key={i}>
                <td className="text-[color:var(--destructive)]">{f.action}</td>
                <td>{f.scope}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      {/* 5 — Opening prompts */}
      <CatalogSection title="Example opening prompts">
        <div className="flex flex-col gap-2 px-3 py-2">
          {OPENING_PROMPTS.map(p => (
            <div key={p.mode} className="text-[var(--text-dense)]">
              <span className="badge-ui badge-status-signed mr-2">{p.mode}</span>
              <code className="font-mono-tabular text-xs text-[var(--muted-foreground)]">{p.example}</code>
            </div>
          ))}
        </div>
      </CatalogSection>
    </div>
  )
}
