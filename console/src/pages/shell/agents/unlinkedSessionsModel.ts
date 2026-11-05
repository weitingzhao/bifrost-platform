import type { AgentThread, AgentThreadsResponse } from '@/api/agentThreads'
import type { PlanSnapshot } from '@/api/planSnapshot'

/**
 * Work → Live's Unlinked sessions (R5:5.5-12 §3.3): the sessions the platform
 * heard from whose work is not in the runtime, from GET /agent/threads. One
 * row per stable identity, host · vendor · session · epoch (§3.1), its latest
 * row standing for it (a key recovered under a new thread id is the same
 * session); a linked session is on Live itself, a Codex hook thread is only
 * counted (its binding waits for S1-0, §2.5).
 */
export type SessionsRead =
  | { state: 'loading' }
  | { state: 'failed'; error: string }
  | { state: 'ok'; data: AgentThreadsResponse; stale: boolean }

export type UnlinkedRow = {
  key: string
  vendor: string
  host: string
  session: string
  epoch: number
  status: AgentThread['status']
  state: 'unlinked' | 'refused' | 'unknown'
  firstSeen: string | undefined
  event: AgentThread['event']
  reason: string
  thread: AgentThread
}

/**
 * Orders two RFC 3339 instants by time, not text: Go writes fractional seconds without trailing zeros, so the strings
 * are not fixed width ("…00.9Z" is earlier than "…00.91Z" but sorts after it as text). Whole seconds by Date.parse, then
 * the fraction to nanoseconds. Unparseable values sort first, and equal ones by text.
 */
export function compareInstant(a: string, b: string): number {
  const split = (v: string): [number, number] => {
    const m = /^(.*?)(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(v)
    const whole = m ? Date.parse(m[1] + m[3]) : NaN
    return [Number.isNaN(whole) ? -Infinity : whole, m?.[2] ? Number(m[2].padEnd(9, '0')) : 0]
  }
  const [aw, af] = split(a)
  const [bw, bf] = split(b)
  if (aw !== bw) return aw < bw ? -1 : 1
  if (af !== bf) return af < bf ? -1 : 1
  return a === b ? 0 : a < b ? -1 : 1
}

/** A session's stable identity (§3.1): an old client's session is its thread id, epoch 0. */
export function sessionKey(t: Pick<AgentThread, 'host' | 'vendor' | 'thread' | 'session' | 'epoch'>): string {
  return `${t.host}/${t.vendor}/${t.session ?? t.thread}/${t.epoch ?? 0}`
}

/**
 * The latest row of each identity, for the event and time it shows. A session's key state (first seen, compactions,
 * the client's diagnosis, and so the binding) is one copy on every row of the identity (the server's shareKeyMeta), so
 * which row is picked never changes it.
 */
export function latestBySession(threads: readonly AgentThread[]): AgentThread[] {
  const latest = new Map<string, AgentThread>()
  for (const t of threads) {
    const k = sessionKey(t)
    const cur = latest.get(k)
    if (!cur || compareInstant(t.at, cur.at) > 0 || (compareInstant(t.at, cur.at) === 0 && `${t.vendor}/${t.thread}` > `${cur.vendor}/${cur.thread}`)) latest.set(k, t)
  }
  return [...latest.values()]
}

const NOTE_WORDS: Record<string, string> = {
  runtime_unreadable: 'The runtime could not be read',
  legacy_no_host: 'A record from before the host was written',
}
const CLIENT_WORDS: Record<string, string> = {
  no_context: 'No plan context', no_description: 'No work description', node_rejected: 'Plan step refused', not_dev_tree: 'Not a dev plan',
  register_failed: 'Registration failed', token_missing: 'No collector token', identity_fallback: 'Session identity was a fallback', no_vendor_session: 'No vendor session id',
}

/** An answer with no binding at all: the platform is a version that does not judge sessions yet (before R5:5.5-12 P-c2). */
export const NO_BINDING_WORDS = 'No binding in this answer: the platform is a version that does not judge sessions'

/**
 * Why a session is not linked, in words: no binding at all said as such (never as "no registration"); the server's
 * note; the client's own reason; a binding Unknown without a note as Unknown; and only a confirmed unlinked as none
 * recorded.
 */
export function unlinkedReason(t: AgentThread): string {
  if (!t.binding) return NO_BINDING_WORDS
  if (t.server_note && NOTE_WORDS[t.server_note]) return NOTE_WORDS[t.server_note]
  const reason = t.client_bind?.reason
  if (reason) return `${t.binding.state === 'refused' ? 'Refused' : 'Client says'} · ${CLIENT_WORDS[reason] ?? reason}`
  if (t.binding.state === 'unknown') return 'Binding Unknown'
  return 'No registration recorded'
}

export type UnlinkedSessions = { rows: UnlinkedRow[]; unsupported: number; runtimeUnreadable: boolean }

/** The section's rows, newest first, and the Codex hook threads only counted. */
export function unlinkedSessions(res: AgentThreadsResponse): UnlinkedSessions {
  const latest = latestBySession(res.threads)
  const rows: UnlinkedRow[] = []
  let unsupported = 0
  for (const t of latest) {
    const state = t.binding?.state ?? 'unknown'
    if (state === 'unsupported') { unsupported++; continue }
    if (state === 'linked') continue
    rows.push({ key: sessionKey(t), vendor: t.vendor, host: t.host, session: t.session ?? t.thread, epoch: t.epoch ?? 0, status: t.status, state,
      firstSeen: t.first_seen_at, event: t.event, reason: unlinkedReason(t), thread: t })
  }
  rows.sort((a, b) => compareInstant(b.thread.at, a.thread.at) || a.key.localeCompare(b.key))
  return { rows, unsupported, runtimeUnreadable: latest.some(t => t.server_note === 'runtime_unreadable') }
}

/** The short form of a session id, as a thread's name has it (agentThreadName: the first eight characters). */
export const shortSession = (session: string) => session.slice(0, 8)

/**
 * The Coverage line's three groups (§3.3): the plan's unregistered work; the sessions linked, unlinked and refused, with
 * the binding rate over them (§9); and the ones not judged, unsupported and unknown. Without sessions (a platform before
 * P-c2) only the first group.
 */
export function coverageGroups(coverage: PlanSnapshot['coverage']): string[] {
  const out = [`${coverage.unregistered_work} unregistered · ${coverage.reconciled ? 'Reconciled' : 'Not reconciled'}`]
  const s = coverage.sessions
  if (!s) return out
  const judged = s.linked + s.unlinked + s.refused
  const rate = judged === 0 ? 'binding rate N/A' : `binding rate ${Math.round(s.linked / judged * 100)}%`
  out.push(`Sessions · ${s.linked} linked · ${s.unlinked} unlinked · ${s.refused} refused · ${rate}`)
  out.push(`Not judged · ${s.unsupported} unsupported · ${s.unknown} unknown`)
  return out
}
