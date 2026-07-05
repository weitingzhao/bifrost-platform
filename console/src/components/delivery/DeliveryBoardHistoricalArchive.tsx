import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
} from '@bifrost/ui'
import { CatalogSection } from '@/components/CatalogSection'
import {
  CHANGE_LOG,
  L1_CHECKS,
  L2_KNOWN_NON_BLOCKERS,
  L2_SESSIONS,
  L3_DECISIONS,
  L4_SIGNOFF,
  MAINLINE_HISTORICAL_PHASES,
  PHASE_L_CONTEXT,
  POST_SIGNOFF_UNLOCK,
} from '@/lib/architecture/deployMainlineCatalog'

export function DeliveryBoardHistoricalArchive() {
  return (
    <>
    <details className="rounded-lg border border-border/50 bg-card">
      <summary className="cursor-pointer list-none px-4 py-3 text-dense-label font-medium text-foreground hover:bg-secondary/30">
        Historical archive — Deploy Mainline phases (seq 0–3, 6)
      </summary>
      <div className="flex flex-col gap-4 border-t border-border/50 px-1 pb-4 pt-2">
        <CatalogSection title="Historical mainline phases">
          <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">
            Completed phases from the migration decision chain. Live spine state: Control Room /
            Agent → Briefing Reconciliation.
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
              {MAINLINE_HISTORICAL_PHASES.map(p => (
                <DenseTableRow key={p.seq}>
                  <DenseTableCell className="font-mono-tabular">{p.seq}</DenseTableCell>
                  <DenseTableCell className="font-medium">{p.phase}</DenseTableCell>
                  <DenseTableCell className="text-dense-meta text-muted-foreground">{p.authority}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant="success">{p.historicalNote ?? '—'}</DenseTag>
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
        </CatalogSection>
      </div>
    </details>

    <details className="rounded-lg border border-border/50 bg-card">
      <summary className="cursor-pointer list-none px-4 py-3 text-dense-label font-medium text-foreground hover:bg-secondary/30">
        Historical archive — Local Prod Final sign-off (2026-06-04 CLOSED)
      </summary>
      <div className="flex flex-col gap-4 border-t border-border/50 px-1 pb-4 pt-2">
        <CatalogSection title="Phase L — Local Prod Final (2C-B pre-gate)">
          <div className="flex flex-col gap-2 px-3 py-2 text-dense-body">
            <p className="m-0">
              <strong>Relation to 2C-A:</strong> {PHASE_L_CONTEXT.relation}
            </p>
            <p className="m-0 text-muted-foreground">{PHASE_L_CONTEXT.purpose}</p>
            <p className="m-0 text-dense-meta font-medium">{PHASE_L_CONTEXT.notEquals}</p>
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
                  <DenseTableCell>
                    <DenseTag variant="success">{c.pass ? 'Pass' : '—'}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{c.agentDate}</DenseTableCell>
                  <DenseTableCell className="text-muted-foreground">{c.remarks}</DenseTableCell>
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
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{s.route}</DenseTableCell>
                  <DenseTableCell>
                    <DenseTag variant="success">{s.pass ? 'Pass' : '—'}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{s.ownerDate}</DenseTableCell>
                  <DenseTableCell className="text-muted-foreground">{s.remarks}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <ul className="m-0 list-disc px-6 py-2 text-dense-meta text-muted-foreground">
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
                  <DenseTableCell className="text-muted-foreground">{d.draft}</DenseTableCell>
                  <DenseTableCell>{d.ownerDecision}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{d.ownerDate}</DenseTableCell>
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
                  <DenseTableCell>
                    <DenseTag variant="success">{s.pass ? 'Pass' : '—'}</DenseTag>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono-tabular text-dense-meta">{s.ownerDate}</DenseTableCell>
                  <DenseTableCell>{s.signee}</DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>
          <p className="m-0 px-3 py-2 text-dense-meta text-muted-foreground">
            Post-signoff: {POST_SIGNOFF_UNLOCK}
          </p>
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
    </details>
    </>
  )
}
