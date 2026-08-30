/**
 * Market Data Plugin API client — via platform-api proxy.
 * Paths: /api/v1/plugins/market-data/api/market/* → Plugin :8790/market/*
 *
 * Auth strategy:
 * - GET (proxyGet): bare fetch — Plugin API is read-only with no auth of its own;
 *   platform-api does not require operator token for GET proxy routes.
 * - POST (proxyPost): authedFetch with PLATFORM_OPERATOR_TOKEN — platform-api mounts
 *   POST /plugins/market-data/api/* under the operator-authed middleware group.
 *   The proxy then attaches MARKET_DATA_WRITE_TOKEN toward Plugin :8790 so the
 *   browser never holds the Plugin write secret.
 */
import { authedFetch } from './client'

const PROXY_BASE = '/api/v1/plugins/market-data/api'

export type MarketDataProxyError = {
  ok: false
  error: string
  status?: number
}

async function proxyGet<T>(pluginPath: string): Promise<T | MarketDataProxyError> {
  const path = `${PROXY_BASE}${pluginPath.startsWith('/') ? pluginPath : `/${pluginPath}`}`
  try {
    const r = await fetch(path)
    const text = await r.text()
    let body: T & { error?: string; detail?: string }
    try {
      body = JSON.parse(text) as T & { error?: string; detail?: string }
    } catch {
      const snippet = text.trim().slice(0, 160) || '(empty body)'
      return {
        ok: false,
        error: `Non-JSON response (HTTP ${r.status}): ${snippet}`,
        status: r.status,
      }
    }
    if (!r.ok) {
      return {
        ok: false,
        error:
          (body as { error?: string }).error ??
          (body as { detail?: string }).detail ??
          `HTTP ${r.status}`,
        status: r.status,
      }
    }
    return body
  } catch (e) {
    return {
      ok: false,
      error: e instanceof Error ? e.message : String(e),
    }
  }
}

async function proxyPost<T>(
  pluginPath: string,
  body: unknown,
): Promise<T | MarketDataProxyError> {
  const path = `${PROXY_BASE}${pluginPath.startsWith('/') ? pluginPath : `/${pluginPath}`}`
  try {
    const r = await authedFetch('market-data enqueue', path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
    const json = (await r.json()) as T & { error?: string; detail?: string }
    if (!r.ok) {
      return {
        ok: false,
        error:
          (json as { error?: string }).error ??
          (json as { detail?: string }).detail ??
          `HTTP ${r.status}`,
        status: r.status,
      }
    }
    return json
  } catch (e) {
    return {
      ok: false,
      error: e instanceof Error ? e.message : String(e),
    }
  }
}

export function isProxyError<T>(
  v: T | MarketDataProxyError,
): v is MarketDataProxyError {
  return (
    v != null &&
    typeof v === 'object' &&
    'ok' in v &&
    (v as MarketDataProxyError).ok === false &&
    'error' in v
  )
}

/* ── Coverage ─────────────────────────────────────────── */

export type QualityCheckItem = {
  check: string
  ok: boolean
  detail?: string
  [key: string]: unknown
}

export type QualityScoreResponse = {
  ok: boolean
  summary?: 'PASS' | 'FAIL' | string
  checks?: QualityCheckItem[]
  error?: string
}

export function fetchQualityScore() {
  return proxyGet<QualityScoreResponse>('/market/coverage/quality-score')
}

export type CoverageDbSummary = {
  ok: boolean
  source?: string
  counts?: Record<string, number | null>
  freshness?: Array<{
    dimension: string
    last_run_at?: string
    rows_written?: number
    status?: string
    updated_at?: string
  }>
  generated_at?: string
  error?: string
}

export type CoverageInventoryMetric = {
  symbols?: number
  days?: number
  latest?: string | null
}

export type CoverageInventoryStockDaily = {
  symbols?: number
  total_rows?: number
  min_date?: string | null
  max_date?: string | null
}

export type CoverageInventoryOption = {
  underlyings?: number
  total_contracts?: number
  total_expiries?: number
  snapshot_symbols?: number
  snapshot_latest?: string | null
  oi_symbols?: number
  oi_latest?: string | null
}

export type CoverageInventoryResponse = {
  ok: boolean
  scope?: string
  watchlist_symbols?: string[]
  stock_daily?: CoverageInventoryStockDaily | null
  stock_min?: CoverageInventoryStockDaily | null
  option?: CoverageInventoryOption | null
  analytics?: {
    max_pain?: CoverageInventoryMetric | null
    atm_iv?: CoverageInventoryMetric | null
    pcr?: CoverageInventoryMetric | null
    iv_percentile?: CoverageInventoryMetric | null
  }
  generated_at?: string
  error?: string
}

export type CoverageWatchlistSymbol = {
  symbol: string
  contract_count?: number
  expiries?: number
  newest_contract_ts?: string | null
}

export type CoverageWatchlist = {
  ok: boolean
  source?: string
  symbols?: CoverageWatchlistSymbol[]
  symbols_count?: number
  error?: string
}

export function fetchCoverageDbSummary() {
  return proxyGet<CoverageDbSummary>('/market/coverage/db-summary')
}

export function fetchCoverageInventory() {
  return proxyGet<CoverageInventoryResponse>('/market/coverage/inventory')
}

export function fetchCoverageWatchlist() {
  return proxyGet<CoverageWatchlist>('/market/coverage/watchlist')
}

export type BarQualityDaily = {
  bar_date?: string
  open?: number | null
  high?: number | null
  low?: number | null
  close?: number | null
  volume?: number | null
  vwap?: number | null
  ohlc_complete?: boolean
}

export type BarQualityDetailResponse = {
  ok: boolean
  symbol?: string
  table?: string
  latest_date?: string | null
  summary?: {
    row_count?: number
    min_date?: string | null
    max_date?: string | null
  }
  daily?: BarQualityDaily[]
  error?: string
}

export type StockDayGapResponse = {
  ok: boolean
  symbol?: string
  lookback_years?: number
  /**
   * Expected trading-day count for the lookback window.
   * Derived from market.us_market_holiday (weekday − NYSE closed). Prefer
   * `missing_dates` / `covered_days` when diagnosing gaps.
   */
  expected_trading_days?: number
  covered_days?: number
  missing_days?: number
  missing_dates?: string[]
  note?: string
  error?: string
}

export type CoverageContractRow = {
  symbol?: string
  contract_count?: number
  expiries?: number
  strikes?: number
  min_expiry?: string | null
  max_expiry?: string | null
  newest_updated_at?: string | null
}

export type CoverageContractsResponse = {
  ok: boolean
  rows?: CoverageContractRow[]
  count?: number
  error?: string
}

export type CoverageGreeksRow = {
  symbol?: string
  total_contracts?: number
  with_iv?: number
  with_delta?: number
  with_full_greeks?: number
  newest_ts?: string | null
}

export type CoverageGreeksResponse = {
  ok: boolean
  rows?: CoverageGreeksRow[]
  count?: number
  symbol?: string | null
  error?: string
}

export type DailyChecklistSymbolItem = {
  symbol?: string
  trade_date?: string
  stock_daily_rows?: number
  option_oi_rows?: number
  corporate_action_rows?: number
}

export type DailyChecklistFreshness = {
  last_run_at?: string | null
  status?: string
  rows_written?: number
}

export type DailyChecklistResponse = {
  ok: boolean
  trade_date?: string
  symbols?: Record<string, DailyChecklistSymbolItem>
  freshness?: Record<string, DailyChecklistFreshness>
  note?: string
  error?: string
}

export type SnapshotQualityDaily = {
  snap_day?: string | null
  contract_count?: number
  iv_pct?: number | null
  full_greeks_pct?: number | null
  oi_pct?: number | null
}

export type SnapshotQualityDetailResponse = {
  ok: boolean
  symbol?: string
  source?: string
  latest_date?: string | null
  daily?: SnapshotQualityDaily[]
  expiries?: unknown[]
  error?: string
}

export type UniverseCountResponse = {
  ok: boolean
  total_tickers?: number
  source?: string
  error?: string
}

export type MarketStatusFreshnessItem = {
  dimension?: string
  last_run_at?: string | null
  status?: string
  rows_written?: number
}

export type MarketStatusResponse = {
  ok: boolean
  service?: string
  db?: string
  polygon_configured?: boolean
  freshness_summary?: MarketStatusFreshnessItem[]
  error?: string
}

export function fetchBarQualityDetail(params: { symbol: string; days?: number }) {
  const q = new URLSearchParams()
  q.set('symbol', params.symbol)
  if (params.days != null) q.set('days', String(params.days))
  return proxyGet<BarQualityDetailResponse>(
    `/market/coverage/bar-quality-detail?${q.toString()}`,
  )
}

export function fetchStockDayGap(params: { symbol: string; years?: number }) {
  const q = new URLSearchParams()
  q.set('symbol', params.symbol)
  if (params.years != null) q.set('years', String(params.years))
  return proxyGet<StockDayGapResponse>(`/market/coverage/stock-day-gap?${q.toString()}`)
}

export function fetchCoverageContracts(params?: { limit?: number }) {
  const q = new URLSearchParams()
  if (params?.limit != null) q.set('limit', String(params.limit))
  const qs = q.toString()
  return proxyGet<CoverageContractsResponse>(
    `/market/coverage/contracts${qs ? `?${qs}` : ''}`,
  )
}

export function fetchCoverageGreeks(params?: { symbol?: string; limit?: number }) {
  const q = new URLSearchParams()
  if (params?.symbol) q.set('symbol', params.symbol)
  if (params?.limit != null) q.set('limit', String(params.limit))
  const qs = q.toString()
  return proxyGet<CoverageGreeksResponse>(`/market/coverage/greeks${qs ? `?${qs}` : ''}`)
}

export function fetchDailyChecklist(params: {
  symbols: string | string[]
  trade_date?: string
}) {
  const q = new URLSearchParams()
  const symbols = Array.isArray(params.symbols)
    ? params.symbols.join(',')
    : params.symbols
  q.set('symbols', symbols)
  if (params.trade_date) q.set('trade_date', params.trade_date)
  return proxyGet<DailyChecklistResponse>(`/market/daily-checklist?${q.toString()}`)
}

export function fetchSnapshotQualityDetail(params: { symbol: string; days?: number }) {
  const q = new URLSearchParams()
  q.set('symbol', params.symbol)
  if (params.days != null) q.set('days', String(params.days))
  return proxyGet<SnapshotQualityDetailResponse>(
    `/market/coverage/snapshot-quality-detail?${q.toString()}`,
  )
}

export function fetchUniverseCount() {
  return proxyGet<UniverseCountResponse>('/market/reference/tickers/universe-count')
}

export function fetchMarketStatus() {
  return proxyGet<MarketStatusResponse>('/market/status')
}

/* ── Readiness (producer quality dashboard) ───────────── */

export type SnapshotByInstrumentType = {
  code: string
  snapshot_row_count: number
  universe_ticker_count: number
}

export type SnapshotCoverageResponse = {
  ok: boolean
  row_count?: number
  last_fetched_at?: string | null
  session_date?: string | null
  by_instrument_type?: SnapshotByInstrumentType[]
  error?: string
}

export type VendorGapRow = {
  symbol?: string
  session_date?: string | null
  last_bar_date?: string | null
  last_bar_close?: number | null
  snapshot_close?: number | null
  reason?: string
}

export type VendorGapResponse = {
  ok: boolean
  gap_count?: number
  /** Present on newer Plugin images — zero-close snapshots excluded from gap_count. */
  zero_snapshot_count?: number
  session_date?: string | null
  gaps?: VendorGapRow[]
  error?: string
}

export type DateCoverageEntry = {
  date?: string
  symbol_count?: number
}

export type DateCoverageResponse = {
  ok: boolean
  low_coverage_dates?: DateCoverageEntry[]
  count?: number
  error?: string
}

export type BarAggregateSymbol = {
  bar_rows?: number
  first_bar_date?: string | null
  last_bar_date?: string | null
  null_close_rows?: number
  null_volume_rows?: number
}

export type BarAggregateResponse = {
  ok: boolean
  /** Present when ?summary=true — totals only, no per-symbol map. */
  summary?: boolean
  symbol_count?: number
  total_bars?: number
  null_close_rows?: number
  null_volume_rows?: number
  symbols?: Record<string, BarAggregateSymbol>
  error?: string
}

export type FinancialsByTypeResponse = {
  ok: boolean
  counts?: {
    income_statement_symbols?: number
    balance_sheet_symbols?: number
    cash_flow_symbols?: number
    ratio_symbols?: number
  }
  error?: string
}

export type FinancialsFillRateResponse = {
  ok: boolean
  tables?: Record<string, Record<string, number>>
  error?: string
}

export type FinancialsCoverageSymbolsResponse = {
  ok: boolean
  income_statement?: Record<string, { q_count?: number; a_count?: number }>
  balance_sheet?: string[]
  cash_flow_statement?: string[]
  ratios?: string[]
  short_interest?: string[]
  short_volume?: string[]
  error?: string
}

export type SepaGapsResponse = {
  ok: boolean
  count?: number
  symbols?: string[]
  note?: string
  error?: string
}

export type ReferenceCoverageResponse = {
  ok: boolean
  total?: number
  filled?: number
  missing?: number
  source?: string
  note?: string
  error?: string
}

export type SepaStatsTable = {
  table?: string
  row_count?: number | null
  latest?: string | null
}

export type SepaStatsResponse = {
  ok: boolean
  tables?: SepaStatsTable[]
  error?: string
}

export function fetchReadinessSnapshotCoverage() {
  return proxyGet<SnapshotCoverageResponse>('/market/readiness/snapshot-coverage')
}

export function fetchReadinessVendorGapDetail(params?: { limit?: number }) {
  const q = new URLSearchParams()
  q.set('detail', 'true')
  if (params?.limit != null) q.set('limit', String(params.limit))
  return proxyGet<VendorGapResponse>(`/market/readiness/vendor-gap?${q.toString()}`)
}

export function fetchReadinessDateCoverage(params?: {
  days_back?: number
  min_symbols?: number
}) {
  const q = new URLSearchParams()
  if (params?.days_back != null) q.set('days_back', String(params.days_back))
  if (params?.min_symbols != null) q.set('min_symbols', String(params.min_symbols))
  const qs = q.toString()
  return proxyGet<DateCoverageResponse>(
    `/market/readiness/date-coverage${qs ? `?${qs}` : ''}`,
  )
}

export function fetchReadinessBarAggregate(params?: {
  window_days?: number
  /** Prefer summary for Ops Console — avoids multi-MiB per-symbol payload. */
  summary?: boolean
}) {
  const q = new URLSearchParams()
  if (params?.window_days != null) q.set('window_days', String(params.window_days))
  if (params?.summary === true) q.set('summary', 'true')
  const qs = q.toString()
  return proxyGet<BarAggregateResponse>(
    `/market/readiness/bar-aggregate${qs ? `?${qs}` : ''}`,
  )
}

export function fetchReadinessFinancialsByType() {
  return proxyGet<FinancialsByTypeResponse>(
    '/market/readiness/financials-by-instrument-type',
  )
}

export function fetchReadinessFinancialsFillRate(params?: {
  universe_symbols?: string | string[]
}) {
  const q = new URLSearchParams()
  if (params?.universe_symbols != null) {
    const syms = Array.isArray(params.universe_symbols)
      ? params.universe_symbols.join(',')
      : params.universe_symbols
    if (syms) q.set('universe_symbols', syms)
  }
  const qs = q.toString()
  return proxyGet<FinancialsFillRateResponse>(
    `/market/readiness/financials-fill-rate${qs ? `?${qs}` : ''}`,
  )
}

export function fetchReadinessFinancialsCoverage() {
  return proxyGet<FinancialsCoverageSymbolsResponse>(
    '/market/readiness/financials-coverage-symbols',
  )
}

export function fetchSepaGaps(params: { report_type: string; limit?: number }) {
  const q = new URLSearchParams()
  q.set('report_type', params.report_type)
  if (params.limit != null) q.set('limit', String(params.limit))
  return proxyGet<SepaGapsResponse>(
    `/market/stocks/fundamentals/sepa/gaps?${q.toString()}`,
  )
}

export function fetchReferenceOverviewCoverage() {
  return proxyGet<ReferenceCoverageResponse>(
    '/market/reference/tickers/overview-coverage',
  )
}

export function fetchReferenceRelatedCoverage() {
  return proxyGet<ReferenceCoverageResponse>(
    '/market/reference/tickers/related-coverage',
  )
}

export function fetchSepaStats() {
  return proxyGet<SepaStatsResponse>('/market/coverage/sepa-stats')
}

/* ── Ingest ───────────────────────────────────────────── */

export type IngestJob = {
  id?: number
  job_id?: string
  kind: string
  payload?: Record<string, unknown>
  status: string
  result?: unknown
  attempts?: number
  max_attempts?: number
  created_at?: string
  updated_at?: string
  started_at?: string
  finished_at?: string
}

export type IngestJobsResponse = {
  ok: boolean
  jobs?: IngestJob[]
  count?: number
  generated_at?: string
  error?: string
}

export type IngestQueueKindCount = {
  kind: string
  pending: number
  running: number
  active: number
}

export type IngestQueueSummaryResponse = {
  ok: boolean
  pending?: number
  running?: number
  active?: number
  kinds?: IngestQueueKindCount[]
  generated_at?: string
  error?: string
}

export type IngestScheduleDrain = {
  started_at?: string | null
  ended_at?: string | null
  active?: boolean
}

export type IngestScheduleSlot = {
  slot: string
  cron: string
  note?: string
  ok?: boolean
  adherence?: string
  detail?: string
  last_fire?: string | null
  next_fires?: string[]
  grace_ends_at?: string
  inline?: boolean
  evidence_kinds?: string[]
  jobs_in_window?: {
    created?: number
    done?: number
    failed?: number
    pending?: number
    running?: number
  }
  freshness_dimension?: string | null
  freshness_last_run_at?: string | null
  fires_in_window?: string[]
  drain?: IngestScheduleDrain | null
}

export type IngestQueueDashboardResponse = {
  ok: boolean
  generated_at?: string
  husbandry?: {
    verdict?: string
    detail?: string
    schedule?: string
    queue?: string
  }
  model?: {
    ready_now?: string
    running?: string
    scheduled_future_jobs?: string
  }
  queue?: {
    pending?: number
    running?: number
    active?: number
    ready_now?: number
    scheduled_future?: number
    oldest_pending_age_sec?: number | null
    verdict?: string
    kinds?: IngestQueueKindCount[]
  }
  throughput?: {
    done_last_5m?: number
    done_last_15m?: number
    done_last_60m?: number
    failed_last_15m?: number
    jobs_per_min_15m?: number
    eta_minutes_at_current_rate?: number | null
  }
  schedule?: {
    verdict?: string
    on_plan?: number
    due?: number
    missed?: number
    grace_minutes?: number
    horizon?: { start?: string; end?: string }
    slots?: IngestScheduleSlot[]
  }
  error?: string
}

export type IngestKindsResponse = {
  ok: boolean
  kinds?: string[]
  error?: string
}

export type EnqueueResponse = {
  ok: boolean
  job_id?: string
  deduplicated?: boolean
  kind?: string
  error?: string
}

export function fetchIngestJobs(params?: { limit?: number; status?: string; kind?: string }) {
  const q = new URLSearchParams()
  if (params?.limit != null) q.set('limit', String(params.limit))
  if (params?.status) q.set('status', params.status)
  if (params?.kind) q.set('kind', params.kind)
  const qs = q.toString()
  return proxyGet<IngestJobsResponse>(`/market/ingest/jobs${qs ? `?${qs}` : ''}`)
}

export function fetchIngestKinds() {
  return proxyGet<IngestKindsResponse>('/market/ingest/kinds')
}

export function fetchIngestQueueSummary() {
  return proxyGet<IngestQueueSummaryResponse>('/market/ingest/queue-summary')
}

export function fetchIngestQueueDashboard(params?: { grace_minutes?: number }) {
  const q = new URLSearchParams()
  if (params?.grace_minutes != null) q.set('grace_minutes', String(params.grace_minutes))
  const qs = q.toString()
  return proxyGet<IngestQueueDashboardResponse>(
    `/market/ingest/queue-dashboard${qs ? `?${qs}` : ''}`,
  )
}

export function enqueueIngestJob(body: {
  kind: string
  payload?: Record<string, unknown>
  priority?: number
}) {
  return proxyPost<EnqueueResponse>('/market/ingest/enqueue', body)
}

/* ── Analytics ────────────────────────────────────────── */

export type AnalyticsListResponse = {
  ok?: boolean
  rows?: Record<string, unknown>[]
  count?: number
  symbol?: string
  trade_date?: string
  error?: string
  [key: string]: unknown
}

export function fetchAnalyticsMaxPain(params?: { symbol?: string; lookback_days?: number }) {
  const q = new URLSearchParams()
  if (params?.symbol) q.set('symbol', params.symbol)
  if (params?.lookback_days != null) q.set('lookback_days', String(params.lookback_days))
  const qs = q.toString()
  return proxyGet<AnalyticsListResponse>(`/market/analytics/max-pain${qs ? `?${qs}` : ''}`)
}

export function fetchAnalyticsAtmIv(params?: { symbol?: string; lookback_days?: number }) {
  const q = new URLSearchParams()
  if (params?.symbol) q.set('symbol', params.symbol)
  if (params?.lookback_days != null) q.set('lookback_days', String(params.lookback_days))
  const qs = q.toString()
  return proxyGet<AnalyticsListResponse>(`/market/analytics/atm-iv${qs ? `?${qs}` : ''}`)
}

export function fetchAnalyticsPcr(params?: { symbol?: string; lookback_days?: number }) {
  const q = new URLSearchParams()
  if (params?.symbol) q.set('symbol', params.symbol)
  if (params?.lookback_days != null) q.set('lookback_days', String(params.lookback_days))
  const qs = q.toString()
  return proxyGet<AnalyticsListResponse>(`/market/analytics/pcr${qs ? `?${qs}` : ''}`)
}

export function fetchAnalyticsIvPercentile(params?: { symbol?: string; lookback_days?: number }) {
  const q = new URLSearchParams()
  if (params?.symbol) q.set('symbol', params.symbol)
  if (params?.lookback_days != null) q.set('lookback_days', String(params.lookback_days))
  const qs = q.toString()
  return proxyGet<AnalyticsListResponse>(
    `/market/analytics/iv-percentile${qs ? `?${qs}` : ''}`,
  )
}

/** Generic JSON probe against any Plugin /market/* GET path. */
export async function probeMarketPath(
  pluginPath: string,
): Promise<{ ok: true; data: Record<string, unknown> } | MarketDataProxyError> {
  const path = pluginPath.startsWith('/market')
    ? pluginPath
    : `/market/${pluginPath.replace(/^\//, '')}`
  const res = await proxyGet<Record<string, unknown>>(path)
  if (isProxyError(res)) return res
  return { ok: true, data: res as Record<string, unknown> }
}
