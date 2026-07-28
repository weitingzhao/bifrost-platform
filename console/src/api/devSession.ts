import { authedFetch } from './client'

export interface DevSession {
  name: string
  label: string
  group: string
  ports?: number[]
  status: string // "running" | "stopped" | "error"
  pid?: number
  uptime_sec?: number
  health_ok?: boolean | null
  restarts?: number
  /** On-disk log size in bytes (from bdev status). */
  log_bytes?: number
  /** Soft cap used by bdev-log-tee (default 5 MiB). */
  log_max_bytes?: number
}

export interface ControlResponse {
  name: string
  action: string
  success: boolean
  message?: string
}

interface LogResponse {
  name: string
  lines: string[]
}

export async function fetchDevSessions(): Promise<DevSession[]> {
  const r = await fetch('/api/v1/dev-sessions')
  if (!r.ok) throw new Error(`dev-sessions: HTTP ${r.status}`)
  return r.json() as Promise<DevSession[]>
}

export async function controlDevSession(
  name: string,
  action: string,
): Promise<ControlResponse> {
  const r = await authedFetch(
    'dev-session control',
    `/api/v1/dev-sessions/${encodeURIComponent(name)}/control`,
    { method: 'POST', body: JSON.stringify({ action }) },
  )
  return r.json() as Promise<ControlResponse>
}

export async function fetchDevSessionLogs(
  name: string,
  lines = 200,
): Promise<string[]> {
  const params = new URLSearchParams({ lines: String(lines) })
  const r = await fetch(`/api/v1/dev-sessions/${encodeURIComponent(name)}/logs?${params}`)
  if (!r.ok) throw new Error(`dev-session logs: HTTP ${r.status}`)
  const data = (await r.json()) as LogResponse
  return data.lines
}
