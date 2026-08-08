import type { QueryClient } from '@tanstack/react-query'
import type {
  LaunchProgramRequest,
  LaunchProgramResponse,
  ProgramDetailResponse,
  ProgramsListResponse,
  CreateProgramFromTemplateRequest,
  PostCompletionDraftItem,
} from './programsTypes'
import { authedFetch, parseError } from './client'

export async function submitProgramPostCompletion(
  programId: string,
  body: {
    new_capabilities?: string[]
    new_risks?: string[]
    operate_queue_items?: PostCompletionDraftItem[]
  },
): Promise<{ program_id: string; submitted_at: string; pending_items: import('./programsTypes').PostCompletionItem[] }> {
  const r = await programsFetch(`/api/v1/programs/${encodeURIComponent(programId)}/complete`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<{
    program_id: string
    submitted_at: string
    pending_items: import('./programsTypes').PostCompletionItem[]
  }>
}

async function programsFetch(path: string, init: RequestInit = {}): Promise<Response> {
  return authedFetch('programs', path, init)
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
  const r = await authedFetch('create program', '/api/v1/programs/from-template', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<ProgramDetailResponse>
}

export async function patchProgramLane(
  programId: string,
  laneId: string,
): Promise<ProgramDetailResponse> {
  const r = await programsFetch(`/api/v1/programs/${encodeURIComponent(programId)}`, {
    method: 'PATCH',
    body: JSON.stringify({ lane_id: laneId }),
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

export async function rejectPostCompletionItem(
  itemId: string,
  body: { reason: string; decision_by?: string },
): Promise<import('./programsTypes').PostCompletionItem> {
  const r = await programsFetch(`/api/v1/programs/post-completion/${encodeURIComponent(itemId)}/reject`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<import('./programsTypes').PostCompletionItem>
}

export async function recordNoPostCompletionHandoff(
  programId: string,
  body: { reason: string; decision_by?: string },
): Promise<ProgramDetailResponse> {
  const r = await programsFetch(
    `/api/v1/programs/${encodeURIComponent(programId)}/post-completion/no-handoff`,
    { method: 'POST', body: JSON.stringify(body) },
  )
  return r.json() as Promise<ProgramDetailResponse>
}

export async function fetchPendingPostCompletion(): Promise<{ items: import('./programsTypes').PostCompletionItem[] }> {
  const r = await fetch('/api/v1/programs/post-completion/pending')
  if (!r.ok) throw await parseError('post-completion pending', r)
  return r.json() as Promise<{ items: import('./programsTypes').PostCompletionItem[] }>
}
