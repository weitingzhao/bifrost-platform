import { useCallback, useState } from 'react'
import { CatalogSection } from '@/components/CatalogSection'
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

function statusBadge(status: string): string {
  if (status.includes('CLOSED')) return 'badge-ui badge-status-signed'
  if (status.includes('In progress') || status.includes('progress')) return 'badge-ui badge-status-pending'
  return 'badge-ui'
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
      <section className="page-section panel-elevated px-4 py-3">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 className="m-0 text-sm font-semibold">Deploy Mainline</h2>
            <p className="m-0 mt-1 max-w-2xl text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Deployment decision chain: Local Prod Final → K3s → Compose → Legacy retirement.
              Source:{' '}
              <code className="font-mono-tabular text-[var(--primary)]">{DEPLOY_MAINLINE_SOURCE}</code>
              {' '}(v{DEPLOY_MAINLINE_VERSION}).
            </p>
            <p className="m-0 mt-2 text-[var(--text-dense-meta)]">{DEPLOY_MAINLINE_STATUS}</p>
          </div>
          <button type="button" className="btn-ui btn-ui-primary shrink-0" onClick={() => void handleCopy()}>
            {copyState === 'copied' ? 'Copied!' : copyState === 'error' ? 'Copy failed' : 'Copy Prompt for LLM'}
          </button>
        </div>
      </section>

      <CatalogSection title="Mainline phases">
        <table className="dense-table">
          <thead>
            <tr>
              <th>#</th>
              <th>Phase</th>
              <th>Authority</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            {MAINLINE_PHASES.map(p => (
              <tr key={p.seq}>
                <td className="font-mono-tabular">{p.seq}</td>
                <td className="font-medium">{p.phase}</td>
                <td className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{p.authority}</td>
                <td><span className={statusBadge(p.status)}>{p.status}</span></td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="Phase L — Local Prod Final (2C-B pre-gate)">
        <div className="flex flex-col gap-2 px-3 py-2 text-[var(--text-dense)]">
          <p className="m-0"><strong>Relation to 2C-A:</strong> {PHASE_L_CONTEXT.relation}</p>
          <p className="m-0 text-[var(--muted-foreground)]">{PHASE_L_CONTEXT.purpose}</p>
          <p className="m-0 text-[var(--text-dense-meta)] font-medium">{PHASE_L_CONTEXT.notEquals}</p>
        </div>
      </CatalogSection>

      <CatalogSection title="L1 — Agent mechanical gate">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Check</th>
              <th>Pass</th>
              <th>Agent date</th>
              <th>Remarks</th>
            </tr>
          </thead>
          <tbody>
            {L1_CHECKS.map(c => (
              <tr key={c.check}>
                <td className="font-medium">{c.check}</td>
                <td><span className="badge-ui badge-status-signed">{c.pass ? 'Pass' : '—'}</span></td>
                <td className="font-mono-tabular text-[var(--text-dense-meta)]">{c.agentDate}</td>
                <td className="text-[var(--muted-foreground)]">{c.remarks}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="L2 — Owner browser short-list">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Session</th>
              <th>Item</th>
              <th>Route</th>
              <th>Owner date</th>
              <th>Remarks</th>
            </tr>
          </thead>
          <tbody>
            {L2_SESSIONS.map((s, i) => (
              <tr key={`${s.session}-${s.item}-${i}`}>
                <td className="font-mono-tabular">{s.session}</td>
                <td className="font-medium">{s.item}</td>
                <td className="font-mono-tabular text-[var(--text-dense-meta)]">{s.route}</td>
                <td className="text-[var(--text-dense-meta)]">{s.ownerDate}</td>
                <td className="text-[var(--muted-foreground)]">{s.remarks || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
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
        <table className="dense-table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Draft proposal</th>
              <th>Owner decision</th>
            </tr>
          </thead>
          <tbody>
            {L3_DECISIONS.map(d => (
              <tr key={d.id}>
                <td className="font-mono-tabular font-medium">{d.id}</td>
                <td className="text-[var(--muted-foreground)]">{d.draft}</td>
                <td>{d.ownerDecision}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>

      <CatalogSection title="L4 — Local Prod Final sign-off">
        <table className="dense-table">
          <thead>
            <tr>
              <th>Item</th>
              <th>Pass</th>
              <th>Owner date</th>
              <th>Signee</th>
            </tr>
          </thead>
          <tbody>
            {L4_SIGNOFF.map(s => (
              <tr key={s.item}>
                <td className="font-medium">{s.item}</td>
                <td><span className="badge-ui badge-status-signed">{s.pass ? 'Pass' : '—'}</span></td>
                <td className="font-mono-tabular text-[var(--text-dense-meta)]">{s.ownerDate}</td>
                <td className="text-[var(--muted-foreground)]">{s.signee}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <p className="m-0 px-3 py-2 text-[var(--text-dense)] text-[var(--muted-foreground)]">
          <strong>Post-signoff unlock:</strong> {POST_SIGNOFF_UNLOCK}
        </p>
      </CatalogSection>

      <div className="grid gap-4 md:grid-cols-2">
        <CatalogSection title="Next: K3s Phase 1 (current priority)">
          <table className="dense-table">
            <thead>
              <tr>
                <th>Target</th>
                <th>Detail</th>
              </tr>
            </thead>
            <tbody>
              {NEXT_K3S_STEPS.map(s => (
                <tr key={s.label}>
                  <td className="font-medium whitespace-nowrap">{s.label}</td>
                  <td className="text-[var(--muted-foreground)]">{s.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
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
        <table className="dense-table">
          <thead>
            <tr>
              <th>Date</th>
              <th>Content</th>
            </tr>
          </thead>
          <tbody>
            {CHANGE_LOG.map(e => (
              <tr key={e.date}>
                <td className="font-mono-tabular whitespace-nowrap">{e.date}</td>
                <td className="text-[var(--muted-foreground)]">{e.content}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </CatalogSection>
    </div>
  )
}
