import {
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  denseTableNumCell,
} from '@bifrost/ui'
import type { DateCoverageEntry } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

export function DateCoverageSection({
  dates,
  count,
  loading,
  error,
  thinDaysIgnored = 0,
}: {
  dates: DateCoverageEntry[]
  count: number
  loading: boolean
  error: string | null
  /** Thin/non-session days filtered out of the producer verdict. */
  thinDaysIgnored?: number
}) {
  const ok = !loading && error == null && count === 0
  const thinNote =
    thinDaysIgnored > 0
      ? ` · ${thinDaysIgnored} thin day${thinDaysIgnored === 1 ? '' : 's'} ignored`
      : ''

  return (
    <OpsSection
      title="Low date coverage"
      description={`Actionable dates only (thin days <500 symbols ignored). Console window 30d / min 100 — Plugin GET /market/readiness/date-coverage${thinNote}`}
      headerExtra={
        loading || error != null ? null : (
          <DenseTag variant={ok ? 'success' : 'warning'}>
            {ok ? 'OK' : `${count} dates`}
          </DenseTag>
        )
      }
      bodyPadding="none"
      overflow="visible"
      collapsible
      defaultCollapsed={ok}
    >
      {loading ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading date coverage…
        </p>
      ) : error != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {error}
        </p>
      ) : dates.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No actionable low-coverage dates
          {thinDaysIgnored > 0
            ? ` (${thinDaysIgnored} thin day${thinDaysIgnored === 1 ? '' : 's'} ignored)`
            : ''}
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Date</DenseTableHead>
              <DenseTableHead className="text-right">Symbol count</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {dates.map(d => (
              <DenseTableRow key={d.date ?? String(d.symbol_count)}>
                <DenseTableCell className="font-mono text-xs">{d.date ?? '—'}</DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {(d.symbol_count ?? 0).toLocaleString('en-US')}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
