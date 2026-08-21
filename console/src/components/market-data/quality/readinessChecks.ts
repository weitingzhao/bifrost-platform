/**
 * Producer readiness acceptance checks for Coverage → Readiness panel.
 * Thresholds align with Ops Console quality-score style (PASS/FAIL + detail).
 */

import type {
  BarAggregateResponse,
  MarketStatusResponse,
  SnapshotCoverageResponse,
  UniverseCountResponse,
} from '@/api/marketDataPlugin'

export type ReadinessCheckId =
  | 'universe'
  | 'snapshot_coverage'
  | 'vendor_gap'
  | 'freshness'
  | 'bar_aggregate'
  | 'date_coverage'

export type ReadinessCheck = {
  id: ReadinessCheckId
  label: string
  ok: boolean
  detail: string
}

export const READINESS_THRESHOLDS = {
  universeMin: 5000,
  snapshotCoveragePct: 95,
  /** Actionable vendor gaps only (snapshot_close > 0). */
  vendorGapMax: 50,
  nullClosePctMax: 1,
  /** stock_daily universe breadth (not watchlist micro-gaps). */
  stockDailyMinSymbols: 4000,
  /**
   * Days with fewer symbols than this are treated as thin/non-session noise
   * (e.g. 15-symbol partial days), not producer coverage failures.
   */
  dateCoverageThinDayMax: 500,
} as const

/** Actionable vendor gap: real snapshot price vs bar divergence (not zero-close SPACs). */
export function isActionableVendorGap(row: {
  snapshot_close?: number | null
  reason?: string
}): boolean {
  const close = Number(row.snapshot_close)
  if (!Number.isFinite(close) || Math.abs(close) < 1e-9) return false
  return true
}

export function partitionVendorGaps(rows: Array<{
  snapshot_close?: number | null
  reason?: string
}>): { actionable: typeof rows; zeroSnapshot: typeof rows } {
  const actionable: typeof rows = []
  const zeroSnapshot: typeof rows = []
  for (const row of rows) {
    if (isActionableVendorGap(row)) actionable.push(row)
    else zeroSnapshot.push(row)
  }
  return { actionable, zeroSnapshot }
}

/** Drop thin/non-session dates from low-coverage list for producer verdict. */
export function filterActionableLowCoverageDates(
  dates: Array<{ date?: string; symbol_count?: number }> | null | undefined,
  thinDayMax: number = READINESS_THRESHOLDS.dateCoverageThinDayMax,
): Array<{ date?: string; symbol_count?: number }> {
  return (dates ?? []).filter(d => Number(d.symbol_count ?? 0) >= thinDayMax)
}

export type SnapshotCoverageDerived = {
  universe: number
  covered: number
  rowCount: number
  coveragePct: number | null
  sessionDate: string | null
}

export function deriveSnapshotCoverage(
  cov: SnapshotCoverageResponse | null | undefined,
): SnapshotCoverageDerived {
  const byType = cov?.by_instrument_type ?? []
  let universe = 0
  let covered = 0
  for (const row of byType) {
    universe += row.universe_ticker_count ?? 0
    covered += row.snapshot_row_count ?? 0
  }
  const rowCount = cov?.row_count ?? 0
  const coveragePct = universe > 0 ? (covered / universe) * 100 : null
  return {
    universe,
    covered,
    rowCount,
    coveragePct,
    sessionDate: cov?.session_date ?? null,
  }
}

export type BarAggregateDerived = {
  symbolCount: number
  totalBars: number
  nullCloseRows: number
  nullClosePct: number | null
}

export function deriveBarAggregate(
  agg: BarAggregateResponse | null | undefined,
): BarAggregateDerived {
  if (agg?.summary === true || (agg?.total_bars != null && agg.symbols == null)) {
    const totalBars = agg.total_bars ?? 0
    const nullCloseRows = agg.null_close_rows ?? 0
    const nullClosePct = totalBars > 0 ? (nullCloseRows / totalBars) * 100 : null
    return {
      symbolCount: agg.symbol_count ?? 0,
      totalBars,
      nullCloseRows,
      nullClosePct,
    }
  }
  const symbols = agg?.symbols ?? {}
  let totalBars = 0
  let nullCloseRows = 0
  let symbolCount = 0
  for (const stats of Object.values(symbols)) {
    symbolCount += 1
    totalBars += stats.bar_rows ?? 0
    nullCloseRows += stats.null_close_rows ?? 0
  }
  const nullClosePct = totalBars > 0 ? (nullCloseRows / totalBars) * 100 : null
  return { symbolCount, totalBars, nullCloseRows, nullClosePct }
}

export function buildReadinessChecks(input: {
  universe: UniverseCountResponse | null
  snapshot: SnapshotCoverageResponse | null
  status: MarketStatusResponse | null
  /** Actionable price divergences only (snapshot_close > 0). */
  actionableVendorGapCount: number
  zeroSnapshotCount: number
  vendorSessionDate?: string | null
  /** Universe stock_daily symbol breadth (producer). */
  stockDailySymbolCount: number | null
  /** Watchlist micro-gaps from quality-score — informational only. */
  watchlistBarGaps: number | null
  /** Low-coverage dates after thin-day filter. */
  actionableLowCoverageDates: number
  thinDaysIgnored: number
  /** Per-check fetch errors — check still rendered as FAIL with this detail. */
  errors?: Partial<Record<ReadinessCheckId, string | null>>
}): ReadinessCheck[] {
  const snap = deriveSnapshotCoverage(input.snapshot)
  const totalTickers = input.universe?.total_tickers ?? 0
  const freshness = input.status?.freshness_summary ?? []
  const freshnessAllOk =
    freshness.length > 0 && freshness.every(f => (f.status ?? '').toLowerCase() === 'ok')
  const errs = input.errors ?? {}

  const coverageOk =
    snap.rowCount > 0 &&
    snap.coveragePct != null &&
    snap.coveragePct >= READINESS_THRESHOLDS.snapshotCoveragePct

  const stockDailyCount = input.stockDailySymbolCount ?? 0
  const stockDailyOk =
    input.stockDailySymbolCount != null &&
    stockDailyCount > READINESS_THRESHOLDS.stockDailyMinSymbols

  function withErr(
    id: ReadinessCheckId,
    label: string,
    ok: boolean,
    detail: string,
  ): ReadinessCheck {
    const err = errs[id]
    if (err) {
      return { id, label, ok: false, detail: `Probe failed: ${err}` }
    }
    return { id, label, ok, detail }
  }

  const zeroNote =
    input.zeroSnapshotCount > 0
      ? ` · ${input.zeroSnapshotCount.toLocaleString('en-US')} zero-close snapshots ignored`
      : ''
  const wlNote =
    input.watchlistBarGaps != null && input.watchlistBarGaps > 0
      ? ` · watchlist gaps ${input.watchlistBarGaps.toLocaleString('en-US')} (Quality Score tab)`
      : ''
  const thinNote =
    input.thinDaysIgnored > 0
      ? ` · ${input.thinDaysIgnored} thin day${input.thinDaysIgnored === 1 ? '' : 's'} ignored (<${READINESS_THRESHOLDS.dateCoverageThinDayMax} symbols)`
      : ''

  return [
    withErr(
      'universe',
      'Universe',
      totalTickers >= READINESS_THRESHOLDS.universeMin,
      `${totalTickers.toLocaleString('en-US')} tickers (min ${READINESS_THRESHOLDS.universeMin.toLocaleString('en-US')})`,
    ),
    withErr(
      'snapshot_coverage',
      'Snapshot coverage',
      coverageOk,
      snap.coveragePct != null
        ? `${snap.covered.toLocaleString('en-US')}/${snap.universe.toLocaleString('en-US')} (${snap.coveragePct.toFixed(1)}%) · rows ${snap.rowCount.toLocaleString('en-US')}${snap.sessionDate ? ` · ${snap.sessionDate}` : ''}`
        : `rows ${snap.rowCount.toLocaleString('en-US')} · no universe breakdown`,
    ),
    withErr(
      'vendor_gap',
      'Vendor gap',
      input.actionableVendorGapCount <= READINESS_THRESHOLDS.vendorGapMax,
      `${input.actionableVendorGapCount.toLocaleString('en-US')} actionable gaps (max ${READINESS_THRESHOLDS.vendorGapMax})${zeroNote}${input.vendorSessionDate ? ` · ${input.vendorSessionDate}` : ''}`,
    ),
    withErr(
      'freshness',
      'Freshness',
      freshnessAllOk,
      freshness.length === 0
        ? 'No freshness_summary from /market/status'
        : `${freshness.filter(f => (f.status ?? '').toLowerCase() === 'ok').length}/${freshness.length} ok`,
    ),
    withErr(
      'bar_aggregate',
      'Stock daily breadth',
      stockDailyOk,
      input.stockDailySymbolCount == null
        ? 'No stock_daily symbol_count'
        : `${stockDailyCount.toLocaleString('en-US')} symbols (need >${READINESS_THRESHOLDS.stockDailyMinSymbols.toLocaleString('en-US')})${wlNote}`,
    ),
    withErr(
      'date_coverage',
      'Date coverage',
      input.actionableLowCoverageDates === 0,
      input.actionableLowCoverageDates === 0
        ? `No actionable low-coverage dates${thinNote}`
        : `${input.actionableLowCoverageDates.toLocaleString('en-US')} low-coverage date${input.actionableLowCoverageDates === 1 ? '' : 's'}${thinNote}`,
    ),
  ]
}

export function readinessOverall(checks: ReadinessCheck[]): {
  pass: boolean
  passCount: number
  total: number
} {
  const passCount = checks.filter(c => c.ok).length
  return { pass: checks.length > 0 && passCount === checks.length, passCount, total: checks.length }
}
