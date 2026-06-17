import { useCallback, useState } from 'react'
import { Button, DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag, type DenseTagVariant } from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import { OpsSection } from '@/components/layout/OpsSection'
import {
  CHANGE_LOG,
  COMPOSE_REFERENCE_COMMANDS,
  DEPLOY_MAINLINE_SOURCE,
  DEPLOY_MAINLINE_STATUS,
  DEPLOY_MAINLINE_VERSION,
  L1_CHECKS,
  L2_KNOWN_NON_BLOCKERS,
  L2_SESSIONS,
  L3_DECISIONS,
  L4_SIGNOFF,
  MAINLINE_PHASES,
  MIGRATION_SEQUENCE,
  NEXT_K3S_STEPS,
  PHASE_L_CONTEXT,
  POST_SIGNOFF_UNLOCK,
  buildDeployMainlineLlmPack,
} from '@/lib/architecture/deployMainlineCatalog'

type CopyState = 'idle' | 'copied' | 'error'

function statusVariant(status: string): DenseTagVariant {
  if (status.includes('CLOSED')) return 'success'
  if (status.includes('In progress') || status.includes('progress')) return 'neutral'
  return 'category'
}

export function DeployMainlinePage() {
  const [copyState, setCopyState] = useState<CopyState>('idle')

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildDeployMainlineLlmPack())
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
            Deployment decision chain: Local Prod Final → K3s → Compose → Legacy retirement.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{DEPLOY_MAINLINE_SOURCE}</code>
            {' '}(v{DEPLOY_MAINLINE_VERSION}).
          </>
        }
        headerExtra={<p className="m-0 mt-2 text-[var(--text-dense-meta)]">{DEPLOY_MAINLINE_STATUS}</p>}
        actions={
          <Button size="sm" className="shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </Button>
        }
        overflow="visible"
      />

      <CatalogSection title="Mainline phases">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>#</DenseTableHead>
              <DenseTableHead>Phase</DenseTableHead>
              <DenseTableHead>Authority</DenseTableHead>
              <DenseTableHead>Status</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {MAINLINE_PHASES.map(p => (
              <DenseTableRow key={p.seq}>
                <DenseTableCell className="font-mono-tabular">{p.seq}</DenseTableCell>
                <DenseTableCell className="font-medium">{p.phase}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{p.authority}</DenseTableCell>
                <DenseTableCell><DenseTag variant={statusVariant(p.status)}>{p.status}</DenseTag></DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="Phase L — Local Prod Final (2C-B pre-gate)">
        <div className="flex flex-col gap-2 px-3 py-2 text-[var(--text-dense)]">
          <p className="m-0"><strong>Relation to 2C-A:</strong> {PHASE_L_CONTEXT.relation}</p>
          <p className="m-0 text-[var(--muted-foreground)]">{PHASE_L_CONTEXT.purpose}</p>
          <p className="m-0 text-[var(--text-dense-meta)] font-medium">{PHASE_L_CONTEXT.notEquals}</p>
        </div>
      </CatalogSection>

      <CatalogSection title="L1 — Agent mechanical gate">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Check</DenseTableHead>
              <DenseTableHead>Pass</DenseTableHead>
              <DenseTableHead>Agent date</DenseTableHead>
              <DenseTableHead>Remarks</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {L1_CHECKS.map(c => (
              <DenseTableRow key={c.check}>
                <DenseTableCell className="font-medium">{c.check}</DenseTableCell>
                <DenseTableCell><DenseTag variant="success">{c.pass ? 'Pass' : '—'}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{c.agentDate}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{c.remarks}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="L2 — Owner browser short-list">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Session</DenseTableHead>
              <DenseTableHead>Item</DenseTableHead>
              <DenseTableHead>Route</DenseTableHead>
              <DenseTableHead>Owner date</DenseTableHead>
              <DenseTableHead>Remarks</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {L2_SESSIONS.map((s, i) => (
              <DenseTableRow key={`${s.session}-${s.item}-${i}`}>
                <DenseTableCell className="font-mono-tabular">{s.session}</DenseTableCell>
                <DenseTableCell className="font-medium">{s.item}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{s.route}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)]">{s.ownerDate}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.remarks || '—'}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <div className="px-3 py-2">
          <p className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">Known non-blockers (inherited from 2C-A)</p>
          <ul className="m-0 list-disc px-4 py-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {L2_KNOWN_NON_BLOCKERS.map(n => (
              <li key={n}>{n}</li>
            ))}
          </ul>
        </div>
      </CatalogSection>

      <CatalogSection title="L3 — Owner decisions (2026-06-04)">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>ID</DenseTableHead>
              <DenseTableHead>Draft proposal</DenseTableHead>
              <DenseTableHead>Owner decision</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {L3_DECISIONS.map(d => (
              <DenseTableRow key={d.id}>
                <DenseTableCell className="font-mono-tabular font-medium">{d.id}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{d.draft}</DenseTableCell>
                <DenseTableCell>{d.ownerDecision}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>

      <CatalogSection title="L4 — Local Prod Final sign-off">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Item</DenseTableHead>
              <DenseTableHead>Pass</DenseTableHead>
              <DenseTableHead>Owner date</DenseTableHead>
              <DenseTableHead>Signee</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {L4_SIGNOFF.map(s => (
              <DenseTableRow key={s.item}>
                <DenseTableCell className="font-medium">{s.item}</DenseTableCell>
                <DenseTableCell><DenseTag variant="success">{s.pass ? 'Pass' : '—'}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{s.ownerDate}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.signee}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">
          <strong>Post-signoff unlock:</strong> {POST_SIGNOFF_UNLOCK}
        </p>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Next: K3s Phase 1 (current priority)">
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Target</DenseTableHead>
                <DenseTableHead>Detail</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {NEXT_K3S_STEPS.map(s => (
                <DenseTableRow key={s.label}>
                  <DenseTableCell className="font-medium whitespace-nowrap">{s.label}</DenseTableCell>
                  <DenseTableCell className="text-[var(--muted-foreground)]">{s.detail}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>

        <CatalogSection title="2C-B Compose (stability reference)">
          <pre className="llm-content-pre m-0 px-3 py-2 text-[var(--text-dense-meta)] font-mono-tabular text-xs">
            {COMPOSE_REFERENCE_COMMANDS.join('\n')}
          </pre>
          <div className="px-3 py-2">
            <p className="m-0 text-xs font-semibold uppercase tracking-wide text-[var(--muted-foreground)]">Migration sequence</p>
            <ul className="m-0 list-disc px-4 py-1 text-[var(--text-dense)]">
              {MIGRATION_SEQUENCE.map(m => (
                <li key={m}>{m}</li>
              ))}
            </ul>
          </div>
        </CatalogSection>
      </div>

      <CatalogSection title="Change log">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Date</DenseTableHead>
              <DenseTableHead>Content</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {CHANGE_LOG.map(e => (
              <DenseTableRow key={e.date}>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap">{e.date}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{e.content}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
