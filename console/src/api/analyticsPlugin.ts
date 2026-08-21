/** Analytics Pipeline (dbt + Elementary) — Ops Console client. */

export type AnalyticsStatus = {
  healthy: boolean
  reachable: boolean
  report_available: boolean
  report_bytes?: number
  last_schedule?: string | null
  cronjob_active: number
  docs_ready: number
  docs_desired: number
  models_total: number
  namespace: string
  error?: string
  hint?: string
  generated_at: string
}

const STATUS_URL = '/api/v1/plugins/analytics/status'
export const ANALYTICS_REPORT_URL = '/api/v1/plugins/analytics/api/elementary_report.html'

export async function fetchAnalyticsStatus(): Promise<AnalyticsStatus> {
  const res = await fetch(STATUS_URL, { credentials: 'same-origin' })
  if (!res.ok) {
    throw new Error(`analytics status HTTP ${res.status}`)
  }
  return (await res.json()) as AnalyticsStatus
}
