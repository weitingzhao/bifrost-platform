import { authHeaders, parseError } from './client'

export interface SessionRecord {
  session_id: string
  program_id: string
  phase_id: string
  lane_id?: string
  pack_hash?: string
  status: string
  created_at: string
  cursor_agent_id?: string
}

export interface CreateSessionRequest {
  session_id?: string
  program_id: string
  phase_id: string
  lane_id?: string
  pack_hash?: string
  pack?: string
  status?: string
  cursor_agent_id?: string
}

/** POST /api/v1/sessions — requires operator token. */
export async function createSession(body: CreateSessionRequest): Promise<SessionRecord> {
  const headers = authHeaders(true)
  const r = await fetch('/api/v1/sessions', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('create session', r)
  return r.json() as Promise<SessionRecord>
}

export const SESSIONS_QUERY_KEY = ['sessions'] as const
