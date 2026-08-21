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
import type { VendorGapRow } from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

function reasonVariant(reason: string | undefined): 'warning' | 'danger' | 'neutral' {
  if (reason === 'vendor_gap') return 'warning'
  if (reason === 'fallback_gap') return 'danger'
  return 'neutral'
}

export function VendorGapDetailTable({
  gaps,
  gapCount,
  loading,
  error,
  sessionDate,
  zeroSnapshotCount = 0,
}: {
  gaps: VendorGapRow[]
  gapCount: number
  loading: boolean
  error: string | null
  sessionDate: string | null
  /** Zero-close snapshots excluded from actionable gap_count (SPAC / pre-open noise). */
  zeroSnapshotCount?: number
}) {
  const ok = !loading && error == null && gapCount === 0
  const zeroNote =
    zeroSnapshotCount > 0
      ? ` · ${zeroSnapshotCount.toLocaleString('en-US')} zero-close ignored`
      : ''

  return (
    <OpsSection
      title="Vendor gap detail"
      description={
        sessionDate
          ? `${gapCount.toLocaleString('en-US')} actionable gaps · session ${sessionDate}${zeroNote}`
          : `${gapCount.toLocaleString('en-US')} actionable gaps — Plugin GET /market/readiness/vendor-gap?detail=true${zeroNote}`
      }
      headerExtra={
        loading || error != null ? null : (
          <DenseTag variant={ok ? 'success' : 'warning'}>
            {ok ? 'OK' : `${gapCount} gaps`}
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
          Loading vendor gaps…
        </p>
      ) : error != null ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {error}
        </p>
      ) : gaps.length === 0 ? (
        <p className="m-0 px-3 py-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No actionable vendor gaps
          {zeroSnapshotCount > 0
            ? ` (${zeroSnapshotCount.toLocaleString('en-US')} zero-close snapshots ignored)`
            : ''}
        </p>
      ) : (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead>Symbol</DenseTableHead>
              <DenseTableHead>Reason</DenseTableHead>
              <DenseTableHead>Last bar</DenseTableHead>
              <DenseTableHead className="text-right">Bar close</DenseTableHead>
              <DenseTableHead className="text-right">Snapshot close</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {gaps.map((g, i) => (
              <DenseTableRow key={`${g.symbol ?? 'x'}-${i}`}>
                <DenseTableCell className="font-mono text-xs font-semibold">
                  {g.symbol ?? '—'}
                </DenseTableCell>
                <DenseTableCell>
                  <DenseTag variant={reasonVariant(g.reason)}>
                    {g.reason ?? 'unknown'}
                  </DenseTag>
                </DenseTableCell>
                <DenseTableCell className="font-mono text-xs">
                  {g.last_bar_date ?? '—'}
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {g.last_bar_close != null ? g.last_bar_close.toFixed(4) : '—'}
                </DenseTableCell>
                <DenseTableCell className={denseTableNumCell}>
                  {g.snapshot_close != null ? g.snapshot_close.toFixed(4) : '—'}
                </DenseTableCell>
              </DenseTableRow>
            ))}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </OpsSection>
  )
}
