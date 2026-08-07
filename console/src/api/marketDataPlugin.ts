/**
 * Market Data Plugin API client — via platform-api proxy.
 * Paths: /api/v1/plugins/market-data/api/market/* → Plugin :8790/market/*
 *
 * Auth strategy:
 * - GET (proxyGet): bare fetch — Plugin API is read-only with no auth of its own;
 *   platform-api does not require operator token for GET proxy routes.
 * - POST (proxyPost): authedFetch with PLATFORM_OPERATOR_TOKEN — platform-api mounts
 *   POST /plugins/market-data/api/* under the operator-authed middleware group
 *   (Enqueue writes to data_ops.job_ingest).
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
    const body = (await r.json()) as T & { error?: string; detail?: string }
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
   * Known issue (2026-08): Plugin may return 0 when `us_trading_calendar` is empty
   * or the calendar lookup fails — do not trust this field for Gap UI until fixed
   * (see market-data-vitals md-vitals-p7). Prefer `missing_dates` / `covered_days`.
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
