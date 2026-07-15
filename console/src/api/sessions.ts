import { getPlatformOperatorToken } from '@/lib/platformAuth'

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

/** POST /api/v1/sessions — requires operator token. */
export async function createSession(body: CreateSessionRequest): Promise<SessionRecord> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch('/api/v1/sessions', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('create session', r)
  return r.json() as Promise<SessionRecord>
}

export const SESSIONS_QUERY_KEY = ['sessions'] as const
