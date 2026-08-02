import type { TelemetryAlertsResponse, TelemetryOverviewResponse, TelemetryTargetsResponse } from './clusterTypes'
import { authedFetch, parseError } from './client'

export type AttentionMuteApiResponse = {
  ok: boolean
  attention_id: string
  expires_at: string
  alertmanager: 'created' | 'skipped' | 'error' | string
  alertmanager_detail?: string
  silence_id?: string
  message: string
}

export async function postAttentionMute(body: {
  attention_id: string
  signal_label: string
  domain: string
  env: string
  alertname?: string
  matchers?: Record<string, string>
  duration_hours?: number
  comment?: string
}): Promise<AttentionMuteApiResponse> {
  const r = await authedFetch('attention mute', '/api/v1/telemetry/attention-mute', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('attention mute', r)
  return r.json() as Promise<AttentionMuteApiResponse>
}

export async function fetchTelemetryOverview(ns?: string): Promise<TelemetryOverviewResponse> {
  const query = ns != null && ns !== '' ? `?ns=${encodeURIComponent(ns)}` : ''
  const r = await fetch(`/api/v1/telemetry/overview${query}`)
  if (!r.ok) {
    const detail = await r.text()
    throw new Error(`telemetry overview: HTTP ${r.status}${detail !== '' ? ` — ${detail}` : ''}`)
  }
  return r.json() as Promise<TelemetryOverviewResponse>
}

export async function fetchTelemetryAlerts(): Promise<TelemetryAlertsResponse> {
  const r = await fetch('/api/v1/telemetry/alerts')
  if (!r.ok) {
    const detail = await r.text()
    throw new Error(`telemetry alerts: HTTP ${r.status}${detail !== '' ? ` — ${detail}` : ''}`)
  }
  return r.json() as Promise<TelemetryAlertsResponse>
}

export async function fetchTelemetryTargets(state?: string): Promise<TelemetryTargetsResponse> {
  const query = state != null && state !== '' ? `?state=${encodeURIComponent(state)}` : ''
  const r = await fetch(`/api/v1/telemetry/targets${query}`)
  if (!r.ok) {
    const detail = await r.text()
    throw new Error(`telemetry targets: HTTP ${r.status}${detail !== '' ? ` — ${detail}` : ''}`)
  }
  return r.json() as Promise<TelemetryTargetsResponse>
}

