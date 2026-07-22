import type { TelemetryAlertsResponse, TelemetryOverviewResponse, TelemetryTargetsResponse } from './clusterTypes'

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

