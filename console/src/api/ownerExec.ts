import { serverFetch } from '@/api/serverContact'

/**
 * owner-exec: the cluster-side runner for owner_run_command cards.
 * The one-time code and the sign-in travel in the body and a header only.
 * They must never be put in a URL: every proxy in front logs request paths.
 */
const RUN_PATH = '/api/owner-exec/run'
const HEALTH_PATH = '/api/owner-exec/health'

export class OwnerExecError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message)
  }
}

/** True when a runner answers on this Console's host. Without one the card keeps its terminal instructions. */
export async function fetchOwnerExecAvailable(signal?: AbortSignal): Promise<boolean> {
  try {
    const r = await serverFetch(HEALTH_PATH, { signal, cache: 'no-store' })
    if (!r.ok) return false
    const body: unknown = await r.json()
    return typeof body === 'object' && body !== null && (body as { ok?: unknown }).ok === true
  } catch {
    return false
  }
}

/** The fingerprint the runner checks the claimed command against: what this page showed. */
export async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))
  return Array.from(new Uint8Array(digest), b => b.toString(16).padStart(2, '0')).join('')
}

export type OwnerExecRunBody = {
  id: string
  command_sha256: string
  code: string
  /** The body an approve would carry; the runner passes it on once the code is good. */
  approve?: { channel: 'console'; approval_line: string; params_hash: string }
}

/** Approve (when pending) and run. Resolves once the command has started. */
export async function postOwnerExecRun(token: string, body: OwnerExecRunBody): Promise<void> {
  const headers = new Headers()
  headers.set('Content-Type', 'application/json')
  headers.set('Authorization', `Bearer ${token}`)
  const r = await serverFetch(RUN_PATH, { method: 'POST', headers, body: JSON.stringify(body) })
  if (r.ok) return
  let message = `The runner refused (${r.status}).`
  try {
    const parsed: unknown = await r.json()
    const text = typeof parsed === 'object' && parsed !== null ? (parsed as { error?: unknown }).error : undefined
    if (typeof text === 'string' && text !== '') message = text
  } catch {
    // not JSON: keep the status line
  }
  throw new OwnerExecError(message, r.status)
}
