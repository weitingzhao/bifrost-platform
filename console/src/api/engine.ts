import type { ReadEvidence } from '@bifrost/ui/city/observation'
import { authHeaders } from '@/api/client'
import { serverFetch } from '@/api/serverContact'

/*
 * Step 0 of the view switch (箭10 2026-11-24, the Console shell's row; UI / Engine leads): the one way the Console
 * reads the UI engine. GET /engine/v1/… with the viewer's own token (the engine forwards it, so it can never read
 * more than the viewer); the envelope opened and its evidence handed on as it came; the identity epoch watched, and
 * when it changes everything read under the old one is dropped (engine-backend.md §4). When the engine cannot be
 * read the answer says "engine unreachable" and why: never a quiet fall back to the platform, so it is always clear
 * which way a view read. Each view's switch stays its own commit and uses this.
 */

/** Who asked, as the engine says it: the epoch changes when the token does (or the engine restarts). */
export type EngineIdentity = { epoch: string; role: string }

/** The engine API v1 envelope (bifrost-engine src/envelope.ts). */
export type EngineEnvelope<T> = {
  version: 1
  model: string
  capturedAt: string
  provenance: 'live'
  identity?: EngineIdentity
  evidence: ReadEvidence[]
  data: T | null
}

export type EngineAnswer<T> =
  | { reached: true; model: string; capturedAt: string; identity: EngineIdentity | null; evidence: ReadEvidence[]; data: T | null }
  | { reached: false; reason: string; status: number | null; lastReachedAt: string | null }

export const ENGINE_PREFIX = '/engine/v1/'

/** The words a view shows when the engine cannot be read. */
export const engineUnreachableText = (a: Extract<EngineAnswer<unknown>, { reached: false }>): string =>
  `engine unreachable: ${a.reason}` + (a.lastReachedAt ? ` (last answer ${a.lastReachedAt})` : ' (no answer yet)')

// --- identity epoch -------------------------------------------------------------------------------------------

let currentEpoch: string | null = null
let lastReachedAt: string | null = null
const epochListeners = new Set<(epoch: string, previous: string | null) => void>()

/** The epoch of the last answer (null before the first: the identity is not ready). */
export const engineEpoch = (): string | null => currentEpoch

/** Called with (new, old) when an answer's epoch differs from the one held; the shell drops the old data there. */
export function onEngineEpochChange(listener: (epoch: string, previous: string | null) => void): () => void {
  epochListeners.add(listener)
  return () => { epochListeners.delete(listener) }
}

function noteEpoch(epoch: string): void {
  if (epoch === currentEpoch) return
  const previous = currentEpoch
  currentEpoch = epoch
  // The first epoch is not a change: nothing was read under another identity yet.
  if (previous !== null) for (const l of epochListeners) l(epoch, previous)
}

/** For tests: forget the epoch and the last answer. */
export function resetEngineIdentity(): void {
  currentEpoch = null
  lastReachedAt = null
}

/** The query key every engine read uses, so the shell can drop all of them at once (['engine', …]). */
export const engineQueryKey = (path: string, query?: Record<string, string>) =>
  ['engine', path, ...(query ? [query] : [])] as const

// --- the read ---------------------------------------------------------------------------------------------------

function isEnvelope(x: unknown): x is EngineEnvelope<unknown> {
  if (typeof x !== 'object' || x === null) return false
  const e = x as Record<string, unknown>
  return e.version === 1 && typeof e.model === 'string' && typeof e.capturedAt === 'string' && Array.isArray(e.evidence) && 'data' in e
}

/**
 * Read `/engine/v1/<path>` as the viewer. The engine answering at all is `reached` (each read inside says how it went,
 * in its evidence); a network failure, a gateway error, or an answer that is not an engine envelope is not.
 */
export async function readEngine<T>(path: string, opts: { query?: Record<string, string>; signal?: AbortSignal } = {}): Promise<EngineAnswer<T>> {
  if (!path.startsWith(ENGINE_PREFIX)) throw new Error(`not an engine read: ${path}`)
  const url = path + (opts.query && Object.keys(opts.query).length > 0 ? '?' + new URLSearchParams(opts.query).toString() : '')
  const unreachable = (reason: string, status: number | null): EngineAnswer<T> => ({ reached: false, reason, status, lastReachedAt })
  let r: Response
  try {
    r = await serverFetch(url, { headers: authHeaders(), ...(opts.signal ? { signal: opts.signal } : {}) })
  } catch (e) {
    if (opts.signal?.aborted) throw e
    return unreachable(e instanceof Error ? e.message : String(e), null)
  }
  let body: unknown
  try {
    body = await r.json()
  } catch {
    return unreachable(`HTTP ${r.status}, not JSON (is /engine routed on this host?)`, r.status)
  }
  if (!r.ok) {
    const why = typeof body === 'object' && body !== null && typeof (body as { error?: unknown }).error === 'string' ? (body as { error: string }).error : ''
    return unreachable(`HTTP ${r.status}${why ? ': ' + why : ''}`, r.status)
  }
  if (!isEnvelope(body)) return unreachable('the answer is not an engine v1 envelope', r.status)
  lastReachedAt = new Date().toISOString()
  const identity = body.identity && typeof body.identity.epoch === 'string' ? body.identity : null
  if (identity) noteEpoch(identity.epoch)
  return { reached: true, model: body.model, capturedAt: body.capturedAt, identity, evidence: body.evidence, data: (body.data ?? null) as T | null }
}
