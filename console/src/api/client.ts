import { getPlatformOperatorToken } from '@/lib/platformAuth'
import { containsSignInData, signInSafeError } from '@/lib/signInError'

/** Current operator bearer token (empty string when unauthenticated). */
export function operatorToken(): string {
  return getPlatformOperatorToken()
}

/** Parse platform-api error JSON into a prefixed Error. */
export async function parseError(prefix: string, r: Response, token = ''): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as {
      error?: string
      message?: string
      detail?: string
      hint?: string
    }
    const raw = JSON.stringify(body)
    if (containsSignInData(raw, token)) return new Error(signInSafeError(raw, token, r.status))
    const head = body.error ?? body.message ?? detail
    const parts = [head]
    if (body.detail != null && body.detail.trim() !== '' && body.detail !== head) {
      parts.push(body.detail.trim())
    }
    if (body.hint != null && body.hint.trim() !== '') {
      parts.push(body.hint.trim())
    }
    detail = parts.join(' — ')
  } catch {
    // keep status detail
  }
  return new Error(signInSafeError(`${prefix}: ${detail}`, token, r.status))
}

/**
 * Authenticated fetch with JSON content-type and Bearer token.
 * Throws a prefixed Error when response is not ok.
 */
export async function authedFetch(
  prefix: string,
  input: RequestInfo | URL,
  init: RequestInit = {},
): Promise<Response> {
  const token = operatorToken()
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  let r: Response
  try {
    r = await fetch(input, { ...init, headers })
  } catch (error) {
    if (error instanceof Error) error.message = signInSafeError(error.message, token)
    throw error
  }
  if (!r.ok) throw await parseError(prefix, r, token)
  return r
}

/** Build Authorization (+ optional JSON Content-Type) headers. */
export function authHeaders(json = false): Headers {
  const headers = new Headers()
  if (json) headers.set('Content-Type', 'application/json')
  const token = operatorToken()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  return headers
}
