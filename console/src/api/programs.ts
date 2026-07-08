import type { QueryClient } from '@tanstack/react-query'
import type {
  LaunchProgramRequest,
  LaunchProgramResponse,
  ProgramDetailResponse,
  ProgramsListResponse,
  CreateProgramFromTemplateRequest,
} from './programsTypes'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as { error?: string; message?: string }
    detail = body.error ?? body.message ?? detail
  } catch {
    // keep status detail
  }
  return new Error(`${prefix}: ${detail}`)
}

async function programsFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const token = getPlatformOperatorToken()
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(path, { ...init, headers })
  if (!r.ok) throw await parseError('programs', r)
  return r
}

export const PROGRAMS_BOARD_QUERY_KEY = ['programs', 'board'] as const

export function invalidateProgramDeliveryQueries(qc: QueryClient, programId: string) {
  void qc.invalidateQueries({ queryKey: PROGRAMS_BOARD_QUERY_KEY })
  void qc.invalidateQueries({ queryKey: ['programs', programId] })
}

export async function fetchDeliveryBoardPrograms(): Promise<ProgramsListResponse> {
  const r = await fetch('/api/v1/programs?board=1')
  if (!r.ok) throw await parseError('programs board', r)
  return r.json() as Promise<ProgramsListResponse>
}

export async function fetchProgramDetail(programId: string): Promise<ProgramDetailResponse> {
  const r = await fetch(`/api/v1/programs/${encodeURIComponent(programId)}`)
  if (!r.ok) throw await parseError('program detail', r)
  return r.json() as Promise<ProgramDetailResponse>
}

export async function signoffProgramPhase(
  programId: string,
  phaseId: string,
  body?: { signed_off_by?: string; signed_off_at?: string; notes?: string },
): Promise<ProgramDetailResponse> {
  const r = await programsFetch(
    `/api/v1/programs/${encodeURIComponent(programId)}/phases/${encodeURIComponent(phaseId)}/signoff`,
    { method: 'POST', body: JSON.stringify(body ?? {}) },
  )
  return r.json() as Promise<ProgramDetailResponse>
}

export async function launchProgramAgent(body: LaunchProgramRequest): Promise<LaunchProgramResponse> {
  const r = await programsFetch('/api/v1/programs/launch', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<LaunchProgramResponse>
}

export async function createProgramFromTemplate(
  body: CreateProgramFromTemplateRequest,
): Promise<ProgramDetailResponse> {
  const r = await programsFetch('/api/v1/programs/from-template', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<ProgramDetailResponse>
}

export async function approvePostCompletionItem(
  itemId: string,
  body?: { approved_by?: string },
): Promise<{ id: string; status: string }> {
  const r = await programsFetch(`/api/v1/programs/post-completion/${encodeURIComponent(itemId)}/approve`, {
    method: 'POST',
    body: JSON.stringify(body ?? {}),
  })
  return r.json() as Promise<{ id: string; status: string }>
}

export async function fetchPendingPostCompletion(): Promise<{ items: import('./programsTypes').PostCompletionItem[] }> {
  const r = await fetch('/api/v1/programs/post-completion/pending')
  if (!r.ok) throw await parseError('post-completion pending', r)
  return r.json() as Promise<{ items: import('./programsTypes').PostCompletionItem[] }>
}
