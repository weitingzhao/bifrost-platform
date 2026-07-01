import { useCallback, useMemo, useState } from 'react'
import { Button, DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag, type DenseTagVariant } from '@bifrost/ui'
import type { OpsContextResponse } from '@/api/types'
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
  MIGRATION_SEQUENCE,
  NEXT_K3S_STEPS,
  PHASE_L_CONTEXT,
  POST_SIGNOFF_UNLOCK,
  buildDeployMainlineLlmPack,
  resolveMainlinePhases,
} from '@/lib/architecture/deployMainlineCatalog'
import { formatSpineStatusLabel } from '@/lib/architecture/spineSemantics'

type CopyState = 'idle' | 'copied' | 'error'

function statusVariant(statusLabel: string, spineStatus?: string): DenseTagVariant {
  if (spineStatus === 'SIGNED' || spineStatus === 'CLOSED') return 'success'
  if (spineStatus === 'IN_PROGRESS' || spineStatus === 'BLOCKED_ON') return 'warning'
  if (statusLabel.includes('CLOSED')) return 'success'
  if (statusLabel.toLowerCase().includes('progress')) return 'neutral'
  return 'category'
}

export function DeployMainlinePage({ context }: { context?: OpsContextResponse }) {
  const [copyState, setCopyState] = useState<CopyState>('idle')
  const phases = useMemo(() => resolveMainlinePhases(context), [context])

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(buildDeployMainlineLlmPack(context))
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }, [context])

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <OpsSection
        title="Overview"
        description={
          <>
            Deployment decision chain: Local Prod Final → K3s → Compose → Legacy retirement.
            Source:{' '}
            <code className="font-mono-tabular text-[var(--primary)]">{DEPLOY_MAINLINE_SOURCE}</code>
            {' '}(v{DEPLOY_MAINLINE_VERSION}). Seq 4/5/7 status from spine (Projection).
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

      <CatalogSection title="Mainline phases (Projection ← spine)">
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Constitution catalog holds phase structure only. Rows 4, 5, 7 bind to spine milestones — status
          labels update from GET /api/v1/context.
        </p>
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
            {phases.map(p => (
              <DenseTableRow key={p.seq}>
                <DenseTableCell className="font-mono-tabular">{p.seq}</DenseTableCell>
                <DenseTableCell className="font-medium">{p.phase}</DenseTableCell>
                <DenseTableCell className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
                  {p.authority}
                </DenseTableCell>
                <DenseTableCell>
                  <div className="flex flex-wrap items-center gap-1.5">
                    <DenseTag variant={statusVariant(p.statusLabel, p.spineStatus)}>{p.statusLabel}</DenseTag>
                    {p.spineMilestoneId != null && p.spineStatus != null && (
                      <DenseTag variant="neutral" className="font-mono-tabular text-[var(--text-dense-caption)]">
                        {p.spineMilestoneId} · {formatSpineStatusLabel(p.spineStatus)}
                      </DenseTag>
                    )}
                  </div>
                </DenseTableCell>
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
              <DenseTableHead>Pass</DenseTableHead>
              <DenseTableHead>Owner date</DenseTableHead>
              <DenseTableHead>Remarks</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {L2_SESSIONS.map(s => (
              <DenseTableRow key={`${s.session}-${s.item}`}>
                <DenseTableCell className="font-mono-tabular">{s.session}</DenseTableCell>
                <DenseTableCell>{s.item}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{s.route}</DenseTableCell>
                <DenseTableCell><DenseTag variant="success">{s.pass ? 'Pass' : '—'}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{s.ownerDate}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{s.remarks}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {L2_KNOWN_NON_BLOCKERS.map(n => (
            <li key={n}>{n}</li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="L3 — Owner decisions">
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>ID</DenseTableHead>
              <DenseTableHead>Draft</DenseTableHead>
              <DenseTableHead>Owner decision</DenseTableHead>
              <DenseTableHead>Date</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {L3_DECISIONS.map(d => (
              <DenseTableRow key={d.id}>
                <DenseTableCell className="font-mono-tabular font-medium">{d.id}</DenseTableCell>
                <DenseTableCell className="text-[var(--muted-foreground)]">{d.draft}</DenseTableCell>
                <DenseTableCell>{d.ownerDecision}</DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{d.ownerDate}</DenseTableCell>
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
                <DenseTableCell>{s.item}</DenseTableCell>
                <DenseTableCell><DenseTag variant="success">{s.pass ? 'Pass' : '—'}</DenseTag></DenseTableCell>
                <DenseTableCell className="font-mono-tabular text-[var(--text-dense-meta)]">{s.ownerDate}</DenseTableCell>
                <DenseTableCell>{s.signee}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Post-signoff: {POST_SIGNOFF_UNLOCK}
        </p>
      </CatalogSection>

      <CatalogSection title="Next: K3s Phase 1">
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense)]">
          {NEXT_K3S_STEPS.map(s => (
            <li key={s.label}>
              <strong>{s.label}</strong> — {s.detail}
            </li>
          ))}
        </ul>
      </CatalogSection>

      <CatalogSection title="Compose reference commands">
        <pre className="m-0 overflow-x-auto px-3 py-2 font-mono-tabular text-[var(--text-dense-meta)]">
          {COMPOSE_REFERENCE_COMMANDS.join('\n')}
        </pre>
      </CatalogSection>

      <CatalogSection title="Migration sequence">
        <ul className="m-0 list-disc px-6 py-2 text-[var(--text-dense)]">
          {MIGRATION_SEQUENCE.map(m => (
            <li key={m}>{m}</li>
          ))}
        </ul>
      </CatalogSection>

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
              <DenseTableRow key={e.date + e.content.slice(0, 24)}>
                <DenseTableCell className="font-mono-tabular whitespace-nowrap">{e.date}</DenseTableCell>
                <DenseTableCell>{e.content}</DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      </CatalogSection>
    </div>
  )
}
