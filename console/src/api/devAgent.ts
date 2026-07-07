import type {
  DevAgentJob,
  DevAgentPersistenceResponse,
  DevAgentProgramDetailResponse,
  DevAgentProgramsResponse,
  DevAgentStatusResponse,
} from './devAgentTypes'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as { error?: string; message?: string; detail?: string }
    detail = body.error ?? body.message ?? detail
    if (body.detail != null && body.detail.trim() !== '' && body.detail !== detail) {
      detail = `${detail} — ${body.detail.trim()}`
    }
  } catch {
    // keep status detail
  }
  return new Error(`${prefix}: ${detail}`)
}

async function devAgentFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = getPlatformOperatorToken()
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(path, { ...init, headers })
  if (!r.ok) throw await parseError('dev-agent', r)
  return r
}

export async function fetchDevAgentStatus(): Promise<DevAgentStatusResponse> {
  const r = await devAgentFetch('/api/v1/programs/active/status')
  return r.json() as Promise<DevAgentStatusResponse>
}

export async function fetchDevAgentPrograms(): Promise<DevAgentProgramsResponse> {
  const r = await devAgentFetch('/api/v1/programs')
  return r.json() as Promise<DevAgentProgramsResponse>
}

export async function fetchDevAgentProgram(programId: string): Promise<DevAgentProgramDetailResponse> {
  const r = await devAgentFetch(`/api/v1/programs/${encodeURIComponent(programId)}`)
  return r.json() as Promise<DevAgentProgramDetailResponse>
}

export async function activateDevAgentProgram(programId: string): Promise<DevAgentProgramDetailResponse> {
  const r = await devAgentFetch(`/api/v1/programs/${encodeURIComponent(programId)}/activate`, {
    method: 'POST',
  })
  return r.json() as Promise<DevAgentProgramDetailResponse>
}

export async function fetchDevAgentPersistence(): Promise<DevAgentPersistenceResponse> {
  const r = await devAgentFetch('/api/v1/programs/active/persistence')
  return r.json() as Promise<DevAgentPersistenceResponse>
}

export async function startDevAgentPhase(phaseId: string): Promise<DevAgentJob> {
  const r = await devAgentFetch('/api/v1/programs/active/start', {
    method: 'POST',
    body: JSON.stringify({ phase_id: phaseId }),
  })
  return r.json() as Promise<DevAgentJob>
}

export async function approveDevAgentPhase(jobId: string): Promise<DevAgentJob> {
  const r = await devAgentFetch(`/api/v1/programs/active/${encodeURIComponent(jobId)}/approve`, {
    method: 'POST',
  })
  return r.json() as Promise<DevAgentJob>
}

export async function rejectDevAgentPhase(jobId: string, feedback: string): Promise<DevAgentJob> {
  const r = await devAgentFetch(`/api/v1/programs/active/${encodeURIComponent(jobId)}/reject`, {
    method: 'POST',
    body: JSON.stringify({ feedback }),
  })
  return r.json() as Promise<DevAgentJob>
}

export async function cancelDevAgent(jobId: string): Promise<DevAgentJob> {
  const r = await devAgentFetch(`/api/v1/programs/active/${encodeURIComponent(jobId)}/cancel`, {
    method: 'POST',
  })
  return r.json() as Promise<DevAgentJob>
}
