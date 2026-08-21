import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  denseTableNumCell,
} from '@bifrost/ui'
import type { SnapshotByInstrumentType } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

function pct(covered: number, universe: number): string {
  if (universe <= 0) return '—'
  return `${((covered / universe) * 100).toFixed(1)}%`
}

export function SnapshotByTypeTable({
  rows,
  loading,
  error,
  sessionDate,
}: {
  rows: SnapshotByInstrumentType[]
  loading: boolean
  error: string | null
  sessionDate: string | null
}) {
  return (
    <OpsSection
      title="Snapshot by instrument type"
      description={
        sessionDate
          ? `Latest session ${sessionDate} — Plugin GET /market/readiness/snapshot-coverage`
          : 'Plugin GET /market/readiness/snapshot-coverage'
      }
      bodyPadding="none"
      overflow="visible"
      collapsible
      defaultCollapsed={rows.length === 0 && !loading && error == null}
    >
      {loading ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading snapshot breakdown…
        </p>
      ) : error != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {error}
        </p>
      ) : rows.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No instrument-type breakdown
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Type</DenseTableHead>
              <DenseTableHead className="text-right">Snapshot</DenseTableHead>
              <DenseTableHead className="text-right">Universe</DenseTableHead>
              <DenseTableHead className="text-right">Coverage</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {rows.map(row => (
              <DenseTableRow key={row.code}>
                <DenseTableCell className="font-mono text-xs">{row.code}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {(row.snapshot_row_count ?? 0).toLocaleString('en-US')}
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {(row.universe_ticker_count ?? 0).toLocaleString('en-US')}
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {pct(row.snapshot_row_count ?? 0, row.universe_ticker_count ?? 0)}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
