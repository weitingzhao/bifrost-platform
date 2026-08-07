import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
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
import {
  fetchBarQualityDetail,
  fetchStockDayGap,
  type BarQualityDetailResponse,
  type StockDayGapResponse,
} from '@/api/marketDataPlugin'
import { OpsSection } from '@/components/layout/OpsSection'

type DepthRowStatus = 'ok' | 'gaps' | 'error' | 'partial'

type DepthRow = {
  symbol: string
  minDate: string | null
  maxDate: string | null
  coveredDays: number | null
  missingDays: number | null
  /** Raw missing_dates from stock-day-gap (do not use expected_trading_days). */
  missingDates: string[]
  status: DepthRowStatus
  error?: string
}

type RecentGapRow = {
  symbol: string
  dates: string[]
}

/** Calendar-day window: today−7d ≤ date ≤ today (UTC YYYY-MM-DD). */
const RECENT_GAP_LOOKBACK_DAYS = 7

function utcTodayIso(now = new Date()): string {
  return now.toISOString().slice(0, 10)
}

function recentGapWindowStart(now = new Date()): string {
  const end = new Date(
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()),
  )
  end.setUTCDate(end.getUTCDate() - RECENT_GAP_LOOKBACK_DAYS)
  return end.toISOString().slice(0, 10)
}

function filterRecentMissingDates(
  dates: string[] | undefined,
  start: string,
  end: string,
): string[] {
  if (!dates?.length) return []
  return dates.filter(d => d >= start && d <= end).sort()
}

function buildRecentGapRows(rows: DepthRow[], now = new Date()): RecentGapRow[] {
  const start = recentGapWindowStart(now)
  const end = utcTodayIso(now)
  const out: RecentGapRow[] = []
  for (const row of rows) {
    const dates = filterRecentMissingDates(row.missingDates, start, end)
    if (dates.length > 0) out.push({ symbol: row.symbol, dates })
  }
  return out
}

function settledValue<T>(
  result: PromiseSettledResult<T | { ok: false; error: string }>,
): T | null {
  if (result.status !== 'fulfilled') return null
  const v = result.value
  if (v != null && typeof v === 'object' && 'ok' in v && (v as { ok: boolean }).ok === false) {
    return null
  }
  return v as T
}

function settledError(result: PromiseSettledResult<unknown>): string | undefined {
  if (result.status === 'rejected') {
    return result.reason instanceof Error ? String(result.reason.message) : String(result.reason)
  }
  const v = result.value
  if (v != null && typeof v === 'object' && 'ok' in v && (v as { ok: boolean }).ok === false) {
    const err = (v as { error?: string }).error
    return err?.trim() || 'Request failed'
  }
  return undefined
}

async function fetchSymbolDepth(symbol: string): Promise<DepthRow> {
  const [barRes, gapRes] = await Promise.allSettled([
    fetchBarQualityDetail({ symbol }),
    fetchStockDayGap({ symbol }),
  ])

  const bar = settledValue<BarQualityDetailResponse>(barRes)
  const gap = settledValue<StockDayGapResponse>(gapRes)
  const barErr = settledError(barRes)
  const gapErr = settledError(gapRes)

  if (bar == null && gap == null) {
    return {
      symbol,
      minDate: null,
      maxDate: null,
      coveredDays: null,
      missingDays: null,
      missingDates: [],
      status: 'error',
      error: barErr ?? gapErr ?? 'Failed',
    }
  }

  const minDate = bar?.summary?.min_date?.trim() || null
  const maxDate = bar?.summary?.max_date?.trim() || null
  const coveredDays =
    typeof gap?.covered_days === 'number'
      ? gap.covered_days
      : typeof bar?.summary?.row_count === 'number'
        ? bar.summary.row_count
        : null
  const missingDays = typeof gap?.missing_days === 'number' ? gap.missing_days : null
  const missingDates = Array.isArray(gap?.missing_dates)
    ? gap.missing_dates.filter((d): d is string => typeof d === 'string' && d.trim() !== '')
    : []

  if (bar == null || gap == null) {
    return {
      symbol,
      minDate,
      maxDate,
      coveredDays,
      missingDays,
      missingDates,
      status: 'partial',
      error: barErr ?? gapErr,
    }
  }

  return {
    symbol,
    minDate,
    maxDate,
    coveredDays,
    missingDays,
    missingDates,
    status: missingDays === 0 ? 'ok' : 'gaps',
  }
}

async function fetchDepthRows(symbols: string[]): Promise<DepthRow[]> {
  const sorted = [...symbols].sort((a, b) => a.localeCompare(b))
  const settled = await Promise.allSettled(sorted.map(sym => fetchSymbolDepth(sym)))
  return settled.map((r, i) => {
    if (r.status === 'fulfilled') return r.value
    return {
      symbol: sorted[i] ?? '?',
      minDate: null,
      maxDate: null,
      coveredDays: null,
      missingDays: null,
      missingDates: [],
      status: 'error' as const,
      error: r.reason instanceof Error ? r.reason.message : String(r.reason),
    }
  })
}

function formatRange(min: string | null, max: string | null): string {
  if (min && max) return `${min} — ${max}`
  if (min) return `${min} — —`
  if (max) return `— — ${max}`
  return '—'
}

function StatusCell({ row }: { row: DepthRow }) {
  if (row.status === 'error') {
    return <DenseTag variant="danger">{row.error?.trim() || 'Error'}</DenseTag>
  }
  if (row.status === 'partial') {
    return (
      <DenseTag variant="warning" title={row.error}>
        Partial
      </DenseTag>
    )
  }
  if (row.status === 'ok' || row.missingDays === 0) {
    return <DenseTag variant="success">OK</DenseTag>
  }
  const n = row.missingDays ?? 0
  return <DenseTag variant="warning">{n} gaps</DenseTag>
}

function buildSummary(rows: DepthRow[]): string {
  const n = rows.length
  const withDays = rows.filter(r => typeof r.coveredDays === 'number')
  const avg =
    withDays.length > 0
      ? Math.round(withDays.reduce((s, r) => s + (r.coveredDays as number), 0) / withDays.length)
      : null

  const mins = rows.map(r => r.minDate).filter((d): d is string => Boolean(d)).sort()
  const maxs = rows.map(r => r.maxDate).filter((d): d is string => Boolean(d)).sort()
  const rangeLo = mins[0]
  const rangeHi = maxs[maxs.length - 1]
  const range = rangeLo && rangeHi ? `${rangeLo} — ${rangeHi}` : rangeLo || rangeHi || '—'

  const zeroGaps = rows.filter(r => r.status === 'ok' || r.missingDays === 0).length
  const gapKnown = rows.filter(r => typeof r.missingDays === 'number').length

  const parts = [`${n} symbols`]
  if (avg != null) parts.push(`avg ${avg.toLocaleString('en-US')} days`)
  parts.push(range)
  if (gapKnown > 0) parts.push(`${zeroGaps}/${gapKnown} zero gaps`)
  return parts.join(' · ')
}

export function StockDepthSection({
  symbols,
  watchlistLoading,
}: {
  symbols: string[]
  watchlistLoading?: boolean
}) {
  const depthQ = useQuery({
    queryKey: ['market-data', 'coverage', 'stock-depth', symbols],
    queryFn: () => fetchDepthRows(symbols),
    enabled: symbols.length > 0,
    refetchInterval: 120_000,
    retry: 1,
  })

  const rows = depthQ.data ?? []
  const summary = useMemo(() => (rows.length > 0 ? buildSummary(rows) : null), [rows])
  const recentGaps = useMemo(() => buildRecentGapRows(rows), [rows])

  const loading = Boolean(watchlistLoading) || (symbols.length > 0 && depthQ.isLoading)

  return (
    <OpsSection
      title="Stock historical depth"
      description="Plugin bar-quality-detail + stock-day-gap for watchlist"
      bodyPadding="default"
      overflow="visible"
      collapsible
      defaultCollapsed={false}
    >
      {loading ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          Loading stock depth…
        </p>
      ) : depthQ.isError ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--destructive)]">
          {depthQ.error instanceof Error ? depthQ.error.message : 'Failed to load stock depth'}
        </p>
      ) : symbols.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No watchlist symbols
        </p>
      ) : rows.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          No depth rows
        </p>
      ) : (
        <>
          {summary != null ? (
            <p className="m-0 mb-3 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {summary}
            </p>
          ) : null}
          <DenseDataTable>
            <DenseTableHeader>
              <DenseTableHeadRow>
                <DenseTableHead>Symbol</DenseTableHead>
                <DenseTableHead>Date Range</DenseTableHead>
                <DenseTableHead className="text-right">Days</DenseTableHead>
                <DenseTableHead className="text-right">Gaps</DenseTableHead>
                <DenseTableHead>Status</DenseTableHead>
              </DenseTableHeadRow>
            </DenseTableHeader>
            <DenseTableBody>
              {rows.map(row => (
                <DenseTableRow key={row.symbol}>
                  <DenseTableCell>
                    <span className="font-semibold font-mono text-entity-symbol">
                      {row.symbol}
                    </span>
                  </DenseTableCell>
                  <DenseTableCell className="font-mono text-[var(--text-dense-meta)]">
                    {formatRange(row.minDate, row.maxDate)}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {row.coveredDays != null ? row.coveredDays.toLocaleString('en-US') : '—'}
                  </DenseTableCell>
                  <DenseTableCell className="text-right font-mono tabular-nums">
                    {row.missingDays != null ? row.missingDays.toLocaleString('en-US') : '—'}
                  </DenseTableCell>
                  <DenseTableCell>
                    <StatusCell row={row} />
                  </DenseTableCell>
                </DenseTableRow>
              ))}
            </DenseTableBody>
          </DenseDataTable>

          <div className="mt-4 border-t border-[var(--border)] pt-3">
            <p className="m-0 mb-2 text-[var(--text-dense-label)] font-medium text-[var(--foreground)]">
              Recent gaps (7 days)
            </p>
            {recentGaps.length === 0 ? (
              <div className="flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)] text-[var(--success)]">
                <span>0 gaps in last 7 days</span>
                <DenseTag variant="success">All clear</DenseTag>
              </div>
            ) : (
              <DenseDataTable>
                <DenseTableHeader>
                  <DenseTableHeadRow>
                    <DenseTableHead>Symbol</DenseTableHead>
                    <DenseTableHead>Missing dates</DenseTableHead>
                  </DenseTableHeadRow>
                </DenseTableHeader>
                <DenseTableBody>
                  {recentGaps.map(gap => (
                    <DenseTableRow key={gap.symbol}>
                      <DenseTableCell>
                        <span className="font-semibold font-mono text-entity-symbol">
                          {gap.symbol}
                        </span>
                      </DenseTableCell>
                      <DenseTableCell className="font-mono text-[var(--text-dense-meta)] text-[var(--destructive)]">
                        {gap.dates.join(', ')}
                      </DenseTableCell>
                    </DenseTableRow>
                  ))}
                </DenseTableBody>
              </DenseDataTable>
            )}
          </div>
        </>
      )}
    </OpsSection>
  )
}
