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

export type CoverageWatchlist = {
  ok: boolean
  symbols?: string[]
  count?: number
  error?: string
}

export function fetchCoverageDbSummary() {
  return proxyGet<CoverageDbSummary>('/market/coverage/db-summary')
}

export function fetchCoverageWatchlist() {
  return proxyGet<CoverageWatchlist>('/market/coverage/watchlist')
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
