import type {
  DevAgentProgramDetailResponse,
  DevAgentProgramJobsResponse,
  DevAgentProgramsResponse,
} from './devAgentTypes'
import { authedFetch } from './client'

async function devAgentFetch(path: string, init: RequestInit = {}): Promise<Response> {
  return authedFetch('dev-agent', path, init)
}

export async function fetchDevAgentPrograms(): Promise<DevAgentProgramsResponse> {
  const r = await devAgentFetch('/api/v1/programs?status=active')
  return r.json() as Promise<DevAgentProgramsResponse>
}

export async function fetchDevAgentProgram(programId: string): Promise<DevAgentProgramDetailResponse> {
  const r = await devAgentFetch(`/api/v1/programs/${encodeURIComponent(programId)}`)
  return r.json() as Promise<DevAgentProgramDetailResponse>
}

export async function fetchDevAgentProgramJobs(programId: string): Promise<DevAgentProgramJobsResponse> {
  const r = await devAgentFetch(`/api/v1/programs/${encodeURIComponent(programId)}/jobs`)
  return r.json() as Promise<DevAgentProgramJobsResponse>
}
