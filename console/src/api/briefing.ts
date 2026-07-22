import { authHeaders, parseError } from './client'

export interface PrepareBriefingRequest {
  session_pack: string
  session_id?: string
  program_id?: string
  phase_id?: string
  lane?: string
  intent?: string
}

export interface PrepareBriefingResponse {
  status: 'ready' | 'error'
  path: string
  meta_path?: string
  message?: string
}

/** POST /api/v1/briefing/prepare — write pack for Cursor IDE /briefing (operator). */
export async function prepareBriefingForIde(
  body: PrepareBriefingRequest,
): Promise<PrepareBriefingResponse> {
  const headers = authHeaders(true)
  const r = await fetch('/api/v1/briefing/prepare', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('prepare briefing', r)
  return r.json() as Promise<PrepareBriefingResponse>
}
