import os from 'node:os'

/** Cursor's per-conversation id. A per-request id is not a session. */
const CURSOR_SESSION_KEYS = ['CURSOR_CONVERSATION_ID', 'CURSOR_SESSION_ID'] as const

/**
 * Codex's own thread id. Codex sets it for its thread; a Codex run that a
 * Claude session dispatches inherits that session's CLAUDE_CODE_HOST_SESSION_ID,
 * so the Codex id is read first: the thread is Codex's, not its parent's
 * (TD-577; 2026-11-21, Station asked whether a Codex seat can use the
 * doorbell, and it could not: no key here was Codex's, so it fell back to the
 * hostname and the doorbell refused).
 */
const CODEX_SESSION_KEY = 'CODEX_THREAD_ID'

/**
 * Requester recorded as X-Bifrost-Session.
 * Codex's thread id, then the Claude host session, then a Cursor conversation
 * id, then the hostname.
 */
export function bifrostSessionId(
  env: NodeJS.ProcessEnv = process.env,
  hostname: () => string = () => os.hostname(),
): string {
  const codex = env[CODEX_SESSION_KEY]?.trim()
  if (codex) return codex
  const claude = env.CLAUDE_CODE_HOST_SESSION_ID?.trim()
  if (claude) return claude
  for (const key of CURSOR_SESSION_KEYS) {
    const value = env[key]?.trim()
    if (value) return value
  }
  return hostname()
}

/** True when no Codex thread, Claude host session or Cursor conversation id is set. */
export function sessionFellBack(env: NodeJS.ProcessEnv = process.env): boolean {
  if (env[CODEX_SESSION_KEY]?.trim()) return false
  if (env.CLAUDE_CODE_HOST_SESSION_ID?.trim()) return false
  for (const key of CURSOR_SESSION_KEYS) {
    if (env[key]?.trim()) return false
  }
  return true
}
