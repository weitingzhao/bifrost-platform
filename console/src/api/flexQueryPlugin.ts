/**
 * IB Flex Query Plugin API client — via platform-api proxy.
 * Paths: /api/v1/plugins/flex-query/api/flex/* → Plugin :8791/flex/*
 *
 * GET is unauthenticated at platform-api. POST enqueue requires operator auth;
 * the proxy attaches FLEX_QUERY_WRITE_TOKEN toward Plugin.
 */
import { authedFetch } from './client'

const PROXY_BASE = '/api/v1/plugins/flex-query/api'

export type FlexQueryProxyError = {
  ok: false
  error: string
  status?: number
}

async function proxyGet<T>(pluginPath: string): Promise<T | FlexQueryProxyError> {
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
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

async function proxyPost<T>(pluginPath: string, body: unknown): Promise<T | FlexQueryProxyError> {
  const path = `${PROXY_BASE}${pluginPath.startsWith('/') ? pluginPath : `/${pluginPath}`}`
  try {
    const r = await authedFetch('flex-query enqueue', path, {
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
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
}

export function isProxyError<T>(v: T | FlexQueryProxyError): v is FlexQueryProxyError {
  return (
    v != null &&
    typeof v === 'object' &&
    'ok' in v &&
    (v as FlexQueryProxyError).ok === false &&
    'error' in v
  )
}

export type FlexIngestJob = {
  id?: number
  kind: string
  payload?: Record<string, unknown>
  status: string
  result?: unknown
  attempts?: number
  max_attempts?: number
  created_at?: string
  started_at?: string
  finished_at?: string
}

export type FlexIngestJobsResponse = {
  jobs?: FlexIngestJob[]
  error?: string
}

export type FlexQueueSummary = {
  pending?: number
  running?: number
  done?: number
  failed?: number
}

export type FlexQueueSlot = {
  slot: string
  kind: string
  cron: string
  last_planned_at?: string | null
  next_fires?: Array<string | null>
  last_job?: FlexIngestJob | null
  late?: boolean
}

export type FlexQueueDashboardResponse = {
  now?: string
  counts?: FlexQueueSummary
  slots?: FlexQueueSlot[]
  error?: string
}

export type FlexKindsResponse = {
  kinds?: string[]
  slots?: string[]
}

export type FlexEnqueueResponse = {
  slot?: string
  kind?: string
  enqueued?: number
  deduped?: number
  job_id?: number | null
  error?: string
}

export type FlexCoverageTable = {
  name: string
  relation?: string
  row_count?: number | null
  latest_ts?: string | null
}

export type FlexCoverageDbSummary = {
  tables?: FlexCoverageTable[]
  error?: string
}

export type FlexFreshnessRow = {
  dimension: string
  latest_ts?: string | null
  row_count?: number | null
  updated_at?: string | null
}

export type FlexFreshnessResponse = {
  dimensions?: FlexFreshnessRow[]
  error?: string
}

export type FlexConfigSummary = {
  tokens: {
    host_token_set: boolean
    host_token_last4: string | null
    secondary_token_set: boolean
    secondary_token_last4: string | null
  }
  range_days: { default: number; init: number }
  query_rows: Array<{
    purpose: string
    query_label: string | null
    query_host_id: string
    query_secondary_id: string | null
  }>
}

export type FlexRawPeekTable = 'executions_raw_flex' | 'transactions'

export type FlexRawPeekResponse = {
  table: string
  row_count: number
  columns: string[]
  rows: unknown[][]
  error?: string
}

export function fetchFlexIngestKinds() {
  return proxyGet<FlexKindsResponse>('/flex/ingest/kinds')
}

export function fetchFlexIngestJobs(params?: { limit?: number; status?: string; kind?: string }) {
  const q = new URLSearchParams()
  if (params?.limit != null) q.set('limit', String(params.limit))
  if (params?.status) q.set('status', params.status)
  if (params?.kind) q.set('kind', params.kind)
  const qs = q.toString()
  return proxyGet<FlexIngestJobsResponse>(`/flex/ingest/jobs${qs ? `?${qs}` : ''}`)
}

export function fetchFlexIngestQueueSummary() {
  return proxyGet<FlexQueueSummary>('/flex/ingest/queue-summary')
}

export function fetchFlexIngestQueueDashboard() {
  return proxyGet<FlexQueueDashboardResponse>('/flex/ingest/queue-dashboard')
}

export function enqueueFlexIngestJob(kind: string, payload?: Record<string, unknown>) {
  return proxyPost<FlexEnqueueResponse>('/flex/ingest/enqueue', { kind, payload: payload ?? {} })
}

export function fetchFlexCoverageDbSummary() {
  return proxyGet<FlexCoverageDbSummary>('/flex/coverage/db-summary')
}

export function fetchFlexCoverageFreshness() {
  return proxyGet<FlexFreshnessResponse>('/flex/coverage/freshness')
}

export function fetchFlexConfigSummary() {
  return proxyGet<FlexConfigSummary>('/flex/config/summary')
}

export function fetchFlexCoverageRawPeek(table: FlexRawPeekTable, limit = 20) {
  const q = new URLSearchParams({ table, limit: String(limit) })
  return proxyGet<FlexRawPeekResponse>(`/flex/coverage/raw-peek?${q.toString()}`)
}
