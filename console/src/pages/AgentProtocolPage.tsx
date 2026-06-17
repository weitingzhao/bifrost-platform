import { useCallback, useState } from 'react'
import { Button, DenseTag, DenseDataTable, DenseTableHeader, DenseTableBody, DenseTableHeadRow, DenseTableRow, DenseTableHead, DenseTableCell } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
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
      <OpsSection
        title="Overview"
        description={
          <>
            Agent interaction modes, context pack layers, forbidden actions, and session startup guidance.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{AGENT_PROTOCOL_SOURCE}</code>
            {' '}(v{AGENT_PROTOCOL_VERSION}).
          </>
        }
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopyForLlm()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      {/* 1 — Agent modes */}
      <CatalogSection title="Agent modes">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Mode</DenseTableHead>
              <DenseTableHead>Flywheel</DenseTableHead>
              <DenseTableHead>Default UI</DenseTableHead>
              <DenseTableHead>Agent may</DenseTableHead>
              <DenseTableHead>Agent must not</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {AGENT_MODES.map(m => (
              <DenseTableRow key={m.mode}>
                <DenseTableCell className="font-medium whitespace-nowrap">
                  <DenseTag variant="success">{m.mode}</DenseTag>
                </DenseTableCell>
                <DenseTableCell>{m.flywheel}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-xs">{m.defaultUI}</DenseTableCell>
                <DenseTableCell>{m.agentMay}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{m.agentMustNot}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
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
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Button</DenseTableHead>
                <DenseTableHead>Contents</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {CONTEXT_PACK_BUTTONS.map(b => (
                <DenseTableRow key={b.button}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{b.button}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{b.contents}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        {/* 3 — Context pack layers */}
        <CatalogSection title="Context pack layers (session startup)">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>#</DenseTableHead>
                <DenseTableHead>Layer</DenseTableHead>
                <DenseTableHead>Description</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {CONTEXT_PACK_LAYERS.map(l => (
                <DenseTableRow key={l.order}>
                  <DenseTableCell className="font-mono-tabular text-center">{l.order}</DenseTableCell>
                  <DenseTableCell className="font-medium whitespace-nowrap">{l.name}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{l.description}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>

      {/* 4 — Forbidden actions */}
      <CatalogSection title="Forbidden actions (all modes)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Action</DenseTableHead>
              <DenseTableHead>Scope</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {FORBIDDEN_ACTIONS.map((f, i) => (
              <DenseTableRow key={i}>
                <DenseTableCell className="text-[color:var(--destructive)]">{f.action}</DenseTableCell>
                <DenseTableCell>{f.scope}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      {/* 5 — Opening prompts */}
      <CatalogSection title="Example opening prompts">
        <div className="flex flex-col gap-2 px-3 py-2">
          {OPENING_PROMPTS.map(p => (
            <div key={p.mode} className="text-[var(--text-dense)]">
              <DenseTag variant="success" className="mr-2">{p.mode}</DenseTag>
              <code className="font-mono-tabular text-xs text-[var(--muted-foreground)]">{p.example}</code>
            </div>
          ))}
        </div>
      </CatalogSection>
    </div>
  )
}
