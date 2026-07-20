import { getPlatformOperatorToken } from '@/lib/platformAuth'

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

/** POST /api/v1/briefing/prepare — write pack for Cursor IDE /briefing (operator). */
export async function prepareBriefingForIde(
  body: PrepareBriefingRequest,
): Promise<PrepareBriefingResponse> {
  const token = getPlatformOperatorToken()
  const headers = new Headers({ 'Content-Type': 'application/json' })
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch('/api/v1/briefing/prepare', {
    method: 'POST',
    headers,
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await parseError('prepare briefing', r)
  return r.json() as Promise<PrepareBriefingResponse>
}
