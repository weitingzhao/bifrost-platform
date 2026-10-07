import os from 'node:os'

/** Cursor's per-conversation id. A per-request id is not a session. */
const CURSOR_SESSION_KEYS = ['CURSOR_CONVERSATION_ID', 'CURSOR_SESSION_ID'] as const

/**
 * Requester recorded as X-Bifrost-Session.
 * Claude host session, then a Cursor conversation id, then the hostname.
 */
export function bifrostSessionId(
  env: NodeJS.ProcessEnv = process.env,
  hostname: () => string = () => os.hostname(),
): string {
  const claude = env.CLAUDE_CODE_HOST_SESSION_ID?.trim()
  if (claude) return claude
  for (const key of CURSOR_SESSION_KEYS) {
    const value = env[key]?.trim()
    if (value) return value
  }
  return hostname()
}
