/**
 * A thread registers itself when its MCP server starts (LANE-agent-tool-comm).
 *
 * The platform registers a thread on its first write; until then nobody can
 * send it a message (HTTP 422 "not a registered actor"). 2026-11-14 this hit
 * twice in one evening: the doorbell test, and seat 9.1 trying to message
 * another seat, which fell back to writing a file. So the full server uploads
 * one small presence artifact naming its thread when it starts: the same body
 * for one thread every time, so after the first start nothing is stored or
 * recorded again (store.UploadArtifact).
 *
 * Only the full server does it (a focus server shares the thread and may hold
 * a viewer token), only against a non-loopback platform (a local dev platform
 * and the tests that start this server against a stub need no registration),
 * and BIFROST_MCP_NO_PRESENCE=1 turns it off.
 */
import { putArtifact, type RuntimeDeps } from './runtimeTools.js'

export function presenceBody(session: string): string {
  return `doorbell: thread:${session} waits for platform messages.\n`
}

function loopback(url: string): boolean {
  try {
    const host = new URL(url).hostname
    return host === '127.0.0.1' || host === 'localhost' || host === '[::1]' || host === '::1'
  } catch {
    return true
  }
}

/** Why presence is not registered here, or undefined when it should be. */
export function presenceSkip(env: Record<string, string | undefined>, focus: string): string | undefined {
  if (env.BIFROST_MCP_NO_PRESENCE === '1') return 'turned off'
  if (focus !== '') return `focus ${focus}`
  if (loopback(env.PLATFORM_API_URL ?? 'http://127.0.0.1:8780')) return 'loopback platform'
  return undefined
}

/** Register this thread; never throws (a failed registration must not stop the server). */
export async function registerPresence(deps: RuntimeDeps): Promise<string> {
  if (deps.fellBack()) return 'no session id'
  try {
    const out = (await putArtifact({ kind: 'report', content: presenceBody(deps.session()), media_type: 'text/plain' }, deps)) as
      | { error?: unknown }
      | undefined
    return out?.error ? `refused: ${JSON.stringify(out).slice(0, 200)}` : 'registered'
  } catch (err) {
    return `failed: ${err instanceof Error ? err.message : String(err)}`
  }
}
