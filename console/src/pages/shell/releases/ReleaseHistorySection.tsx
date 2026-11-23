import { DenseDataTable, DenseTableBody, DenseTableCell, DenseTableHead, DenseTableHeadRow, DenseTableHeader, DenseTableRow, DenseTag } from '@bifrost/ui'
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { ReleaseListResponse, ReleaseRecord } from '@/api/releases'
import { serverFetch } from '@/api/serverContact'
import { LedgerGrid } from '@bifrost/ui/city/ledger'
import { OpsSection, OpsSubsectionTitle } from '@/components/layout/OpsSection'
import {
  historyScope,
  HISTORY_LIMIT,
  releaseLedgerTable,
  shortSha,
  type SupersededRun,
} from '@/pages/shell/releases/releaseView'

export type ReleaseHistoryProps = {
  records: ReleaseRecord[]
  recordsLoading: boolean
  recordsError: string | null
  superseded: SupersededRun[]
}

function messageRow(colSpan: number, text: string) {
  return (
    <DenseTableRow>
      <DenseTableCell colSpan={colSpan} className="text-[var(--muted-foreground)]">
        {text}
      </DenseTableCell>
    </DenseTableRow>
  )
}

export function ReleaseHistorySection({ records, recordsLoading, recordsError, superseded }: ReleaseHistoryProps) {
  // The page reads the newest 40 for its lanes; History reads every record the platform keeps (the list takes any
  // ?limit=, no cursor), and only once the section is opened (箭10 2026-11-23: PROD keeps 1673, the table showed 40).
  const [open, setOpen] = useState(false)
  const history = useQuery({
    queryKey: ['releases', 'records', 'history', HISTORY_LIMIT],
    queryFn: async ({ signal }) => {
      const r = await serverFetch(`/api/v1/releases?limit=${HISTORY_LIMIT}`, { signal })
      if (!r.ok) throw new Error(`releases: HTTP ${r.status}`)
      return (await r.json()) as ReleaseListResponse
    },
    enabled: open,
    // PROD keeps 1673 records (1.1 MB on 2026-11-23); the list has no cursor or total yet (asked of delivery), so it is
    // read up to HISTORY_LIMIT on opening and refreshed slowly, not every minute.
    refetchInterval: 5 * 60_000,
  })
  const all = history.data && !history.data.error ? history.data.records : null
  const shown = all ?? records
  const historyError = history.error instanceof Error ? history.error.message : history.data?.error || null
  const count = recordsLoading && all == null ? '…' : String(shown.length + superseded.length)
  return (
    <OpsSection
      title={`History · ${count}`}
      description="Finished releases, and failed runs a later success of the same pipeline superseded."
      collapsible
      defaultCollapsed
      onOpenChange={setOpen}
      bodyPadding="none"
      overflow="visible"
      bodyClassName="ops-section-body--table"
    >
      <OpsSubsectionTitle className="px-3 pt-2">Recent releases</OpsSubsectionTitle>
      <p className="m-0 px-3 pb-1 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
        Deploying runs roll out; image builds are deployed later by a pin or GitOps sync.
      </p>
      {recordsError != null && recordsError !== '' && (
        <p className="m-0 px-3 py-2 text-[var(--text-dense-meta)] text-[var(--destructive)]">{recordsError}</p>
      )}
      <div className="min-w-0 px-3 py-2">
        {recordsLoading ? (
          <p className="m-0 text-[var(--muted-foreground)]">…</p>
        ) : (
          <LedgerGrid
            table={releaseLedgerTable(shown)}
            selected=""
            onSelect={() => {}}
            scope={historyScope({ shown: shown.length, full: all != null, reading: open && history.isLoading, error: historyError })}
          />
        )}
      </div>

      <OpsSubsectionTitle className="px-3 pt-3">Superseded failures</OpsSubsectionTitle>
      <DenseDataTable>
        <DenseTableHeader>
          <DenseTableHeadRow>
            <DenseTableHead>Pipeline</DenseTableHead>
            <DenseTableHead>Run</DenseTableHead>
            <DenseTableHead>Status</DenseTableHead>
            <DenseTableHead>Started</DenseTableHead>
            <DenseTableHead>Revision</DenseTableHead>
            <DenseTableHead>Superseded by</DenseTableHead>
          </DenseTableHeadRow>
        </DenseTableHeader>
        <DenseTableBody>
          {superseded.length === 0
            ? messageRow(6, 'No superseded failures.')
            : superseded.map(run => (
                <DenseTableRow key={`${run.pipeline}/${run.name}`}>
                  <DenseTableCell className="font-mono-tabular">{run.pipeline}</DenseTableCell>
                  <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={run.name}>
                    {run.name}
                  </DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap">
                    <DenseTag variant="neutral">superseded</DenseTag>{' '}
                    <span className="text-[var(--muted-foreground)]">{run.status}</span>
                  </DenseTableCell>
                  <DenseTableCell className="whitespace-nowrap font-mono-tabular">{run.started}</DenseTableCell>
                  <DenseTableCell className="font-mono-tabular">{shortSha(run.revision) || run.revision}</DenseTableCell>
                  <DenseTableCell className="max-w-[260px] truncate font-mono-tabular" title={run.supersededBy}>
                    {run.supersededBy}
                  </DenseTableCell>
                </DenseTableRow>
              ))}
        </DenseTableBody>
      </DenseDataTable>
    </OpsSection>
  )
}
