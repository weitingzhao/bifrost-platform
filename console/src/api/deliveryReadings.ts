import { authedFetch } from './client'

export type DeliveryReading = {
  metric_id: string; definition_version: number; definition_sha256: string
  scope_kind: string; scope_ref: string; label: string; window_kind: string
  window_start: string; window_end: string; source: string; unit: string
  samples: number; value: number | null; stats: Record<string, unknown>; conclusive: boolean
}
export type DeliveryComparison = {
  metric_id: string; baseline: DeliveryReading; current: DeliveryReading
  same_definition: boolean; conclusive: boolean; change: number | null
}
export const DELIVERY_METRICS = ['lead_time_s', 'owner_wait_share', 'macro_doing_s', 'macro_owner_wait_s', 'macro_idle_s', 'ci_runs_per_commit'] as const
export async function fetchDeliveryReadings(metric: string, signal?: AbortSignal): Promise<DeliveryReading[]> {
  const response = await authedFetch('Delivery metrics', `/api/v1/agentrt/metrics/readings?${new URLSearchParams({ metric, window_kind: 'span', limit: '500' })}`, { signal })
  return (await response.json()).readings
}
export async function fetchDeliveryComparison(metric: string, label: string, signal?: AbortSignal): Promise<DeliveryComparison> {
  const response = await authedFetch('Delivery metrics', `/api/v1/agentrt/metrics/compare?${new URLSearchParams({ metric, label })}`, { signal })
  return response.json()
}
export function readingValue(r: DeliveryReading): string {
  if (r.value === null) return 'unknown'
  if (r.unit === 'seconds') return `${Number((r.value / 3600).toFixed(2))} h`
  if (r.unit === 'ratio') return `${Number((r.value * 100).toFixed(1))}%`
  return String(r.value)
}
