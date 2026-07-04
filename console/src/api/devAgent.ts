import type { DevAgentJob, DevAgentStatusResponse } from './devAgentTypes'

const operatorToken = (): string =>
  localStorage.getItem('bifrost_ops_token') ?? ''

async function devAgentFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = operatorToken()
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(path, { ...init, headers })
  if (!r.ok) {
    const text = await r.text().catch(() => '')
    throw new Error(`dev-agent: HTTP ${r.status} — ${text}`)
  }
  return r
}

export async function fetchDevAgentStatus(): Promise<DevAgentStatusResponse> {
  const r = await devAgentFetch('/api/v1/dev-agent/status')
  return r.json() as Promise<DevAgentStatusResponse>
}

export async function startDevAgentPhase(phaseId: string): Promise<DevAgentJob> {
  const r = await devAgentFetch('/api/v1/dev-agent/start', {
    method: 'POST',
    body: JSON.stringify({ phase_id: phaseId }),
  })
  return r.json() as Promise<DevAgentJob>
}

export async function approveDevAgentPhase(jobId: string): Promise<DevAgentJob> {
  const r = await devAgentFetch(`/api/v1/dev-agent/${encodeURIComponent(jobId)}/approve`, {
    method: 'POST',
  })
  return r.json() as Promise<DevAgentJob>
}

export async function rejectDevAgentPhase(jobId: string, feedback: string): Promise<DevAgentJob> {
  const r = await devAgentFetch(`/api/v1/dev-agent/${encodeURIComponent(jobId)}/reject`, {
    method: 'POST',
    body: JSON.stringify({ feedback }),
  })
  return r.json() as Promise<DevAgentJob>
}

export async function cancelDevAgent(jobId: string): Promise<DevAgentJob> {
  const r = await devAgentFetch(`/api/v1/dev-agent/${encodeURIComponent(jobId)}/cancel`, {
    method: 'POST',
  })
  return r.json() as Promise<DevAgentJob>
}
