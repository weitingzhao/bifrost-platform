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
import type { ReactNode } from 'react'
import type { DateCoverageEntry } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

export function DateCoverageSection({
  dates,
  count,
  loading,
  error,
  thinDaysIgnored = 0,
  windowDays,
  action,
}: {
  dates: DateCoverageEntry[]
  count: number
  loading: boolean
  error: string | null
  /** Thin/non-session days filtered out of the producer verdict. */
  thinDaysIgnored?: number
  /** The window this reading asked for — a limit nobody states reads as "there is nothing older". */
  windowDays: number
  /** The refill for this reading, on the reading itself. */
  action?: ReactNode
}) {
  const ok = !loading && error == null && count === 0
  const thinNote =
    thinDaysIgnored > 0
      ? ` · ${thinDaysIgnored} thin day${thinDaysIgnored === 1 ? '' : 's'} ignored`
      : ''

  return (
    <OpsSection
      title="Low date coverage"
      description={`Actionable dates only (thin days <500 symbols ignored). Window ${windowDays}d back / min 100 — nothing older is asked about. Plugin GET /market/readiness/date-coverage${thinNote}`}
      headerExtra={
        <div className="flex items-center gap-2">
          {loading || error != null ? null : (
            <DenseTag variant={ok ? 'success' : 'warning'}>
              {ok ? 'OK' : `${count} dates`}
            </DenseTag>
          )}
          {action}
        </div>
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
