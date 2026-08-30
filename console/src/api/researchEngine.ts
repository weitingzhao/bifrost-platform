/**
 * Research Engine (bifrost-research :8795) — Ops Console client via platform proxy.
 *
 * Platform routes (Wave 2):
 *   GET /api/v1/research/status
 *   GET /api/v1/research/*  → Research API /*
 */

export type ResearchStatus = {
  reachable: boolean
  error?: string
  hint?: string
  generated_at: string
}

export type ForecastSettlementRow = {
  settlement_id: string
  session_id: string
  symbol: string
  trade_date: string
  expected_close: number | null
  actual_close: number | null
  close_miss: number | null
  close_miss_pct: number | null
  path_hit: boolean | null
  path_hit_count: number | null
  path_total: number | null
  notes?: string | null
  computed_at?: string | null
}

export type ForecastSessionRow = {
  session_id: string
  symbol: string
  trade_date: string
  regime?: string | null
  spot?: number | null
  expected_close?: number | null
  llm_provider?: string | null
  advisory?: string | null
  computed_at?: string | null
  /** Optional when Research API persists token metadata (future). */
  token_usage?: number | null
  token_cost_usd?: number | null
  prompt_tokens?: number | null
  completion_tokens?: number | null
}

export type SettlementListResponse = {
  rows: ForecastSettlementRow[]
  count: number
}

export type ForecastSessionListResponse = {
  rows: ForecastSessionRow[]
  count: number
}

export type ElementaryStatus = {
  ok: boolean
  present: boolean
  path?: string
  mtime?: string | null
}

export type ResearchProxyError = {
  error: string
  hint?: string
}

const STATUS_URL = '/api/v1/research/status'
const RESEARCH_PROXY = '/api/v1/research'

export function isResearchProxyError(data: unknown): data is ResearchProxyError {
  return (
    typeof data === 'object' &&
    data != null &&
    'error' in data &&
    typeof (data as ResearchProxyError).error === 'string' &&
    !('rows' in data) &&
    !('reachable' in data) &&
    !('ok' in data)
  )
}

async function researchGet<T>(path: string): Promise<T | ResearchProxyError> {
  const res = await fetch(`${RESEARCH_PROXY}${path}`, { credentials: 'same-origin' })
  const body = (await res.json().catch(() => null)) as unknown
  if (!res.ok) {
    if (body && typeof body === 'object' && 'error' in body) {
      return body as ResearchProxyError
    }
    return {
      error: `Research API HTTP ${res.status}`,
      hint: 'Ensure research-api is Running, or set RESEARCH_API_URL',
    }
  }
  return body as T
}

export type ResearchHealth = {
  version?: string
  startup_ok?: boolean
}

export async function fetchResearchHealth(): Promise<ResearchHealth | ResearchProxyError> {
  return researchGet<ResearchHealth>('/health')
}

export async function fetchResearchStatus(): Promise<ResearchStatus> {
  const res = await fetch(STATUS_URL, { credentials: 'same-origin' })
  if (!res.ok) {
    throw new Error(`research status HTTP ${res.status}`)
  }
  return (await res.json()) as ResearchStatus
}

export async function fetchForecastSettlements(
  limit = 50,
): Promise<SettlementListResponse | ResearchProxyError> {
  return researchGet<SettlementListResponse>(`/research/backtest/settlement?limit=${limit}`)
}

export async function fetchForecastSessions(
  limit = 50,
): Promise<ForecastSessionListResponse | ResearchProxyError> {
  return researchGet<ForecastSessionListResponse>(`/research/forecast/sessions?limit=${limit}`)
}

export async function fetchElementaryStatus(): Promise<ElementaryStatus | ResearchProxyError> {
  return researchGet<ElementaryStatus>('/analytics/elementary')
}

export type SignalHealthFreshnessRow = {
  label: string
  table?: string
  status: string
  age_hours?: number | null
  max_computed_at?: string | null
  row_count?: number
}

export type SignalHealthData = {
  overall: string
  as_of?: string
  freshness: SignalHealthFreshnessRow[]
  extra_tables?: SignalHealthFreshnessRow[]
}

export type SignalHealthResponse = {
  ok: boolean
  data: SignalHealthData
}

export type OrchestrationStatusData = {
  verdict: string
  job_name: string
  last_run_status?: string | null
  last_run_ended_at?: string | null
  last_run_id?: string | null
  overdue: boolean
  detail: string
  as_of?: string
}

export type OrchestrationStatusResponse = {
  ok: boolean
  data: OrchestrationStatusData
}

export async function fetchSignalHealth(): Promise<SignalHealthResponse | ResearchProxyError> {
  return researchGet<SignalHealthResponse>('/research/signal-health')
}

export async function fetchOrchestrationStatus(): Promise<
  OrchestrationStatusResponse | ResearchProxyError
> {
  return researchGet<OrchestrationStatusResponse>('/research/orchestration/status')
}

export type AccuracySummary = {
  sessionsSettled: number
  pathHitRate: number | null
  avgCloseMissPct: number | null
  medianCloseMissPct: number | null
  pathHitSessions: number
}

/** Client-side aggregate from settlement rows (platform proxy is GET-only). */
export function summarizeSettlements(rows: ForecastSettlementRow[]): AccuracySummary {
  if (rows.length === 0) {
    return {
      sessionsSettled: 0,
      pathHitRate: null,
      avgCloseMissPct: null,
      medianCloseMissPct: null,
      pathHitSessions: 0,
    }
  }
  const pathHitSessions = rows.filter(r => r.path_hit === true).length
  const missPcts = rows
    .map(r => r.close_miss_pct)
    .filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const avg =
    missPcts.length > 0 ? missPcts.reduce((a, b) => a + b, 0) / missPcts.length : null
  const sorted = [...missPcts].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  const median =
    sorted.length === 0
      ? null
      : sorted.length % 2 === 0
        ? ((sorted[mid - 1] ?? 0) + (sorted[mid] ?? 0)) / 2
        : (sorted[mid] ?? null)

  return {
    sessionsSettled: rows.length,
    pathHitRate: pathHitSessions / rows.length,
    avgCloseMissPct: avg,
    medianCloseMissPct: median,
    pathHitSessions,
  }
}
