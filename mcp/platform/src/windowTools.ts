/**
 * The release window from an agent's side (LANE-agent-tool-comm, locks and
 * queue design v1): read it, take it or queue for it, give it back. The
 * caller is this session: who = its session id, and a queued caller's
 * thread:<session> gets "window.your_turn" when the window is reserved for it
 * (2 minutes by default; take it with hold_release_window again). Pair it
 * with the doorbell (mcp/platform/src/doorbell.ts) to sleep until then.
 */
import { bifrostSessionId, sessionFellBack } from './sessionId.js'
import { platformDelete, platformGet, platformPut } from './platformClient.js'

export const WINDOW_PATH = '/api/v1/delivery/release-window'

export interface WindowDeps {
  get: (path: string) => Promise<unknown>
  put: (path: string, body: unknown) => Promise<unknown>
  del: (path: string) => Promise<unknown>
  session: () => string
  fellBack: () => boolean
}

export function defaultWindowDeps(): WindowDeps {
  return {
    get: platformGet,
    put: platformPut,
    del: platformDelete,
    session: () => bifrostSessionId(),
    fellBack: () => sessionFellBack(),
  }
}

const NO_SESSION = { error: 'no session id', hint: 'The window names its holder by your session; run inside a harness session (CLAUDE_CODE_HOST_SESSION_ID).' }

export async function getReleaseWindow(d: WindowDeps = defaultWindowDeps()): Promise<unknown> {
  return d.get(WINDOW_PATH)
}

export interface HoldInput {
  what: string
  reason?: string
  ttl_minutes?: number
  /** Default true: wait behind a live holder (HTTP 202 with your position) instead of being refused. */
  queue?: boolean
  queue_minutes?: number
  /** One line you will act on when it is your turn; it comes back in window.your_turn. */
  resume?: string
}

export async function holdReleaseWindow(input: HoldInput, d: WindowDeps = defaultWindowDeps()): Promise<unknown> {
  if (d.fellBack()) return NO_SESSION
  const session = d.session()
  const queue = input.queue ?? true
  const body: Record<string, unknown> = { who: session, what: input.what }
  if (input.reason) body.reason = input.reason
  if (input.ttl_minutes != null) body.ttl_minutes = input.ttl_minutes
  if (queue) {
    body.queue = true
    body.thread = `thread:${session}`
    if (input.queue_minutes != null) body.queue_minutes = input.queue_minutes
    if (input.resume) body.resume = input.resume
  }
  return d.put(WINDOW_PATH, body)
}

export async function releaseReleaseWindow(d: WindowDeps = defaultWindowDeps()): Promise<unknown> {
  if (d.fellBack()) return NO_SESSION
  return d.del(`${WINDOW_PATH}?who=${encodeURIComponent(d.session())}`)
}
