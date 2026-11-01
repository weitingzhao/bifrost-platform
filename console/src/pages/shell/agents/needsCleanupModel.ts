import type { RuntimeHost, RuntimeProgress } from '@/api/agentRuntime'
import type { ExternalWork, PlanSnapshot } from '@/api/planSnapshot'
import { offlineHosts } from '../needs-you/mergedNeedsYou'
import { ago } from '../owner/ownerModel'
import { SHELL_POLL_MS } from './moduleDetailModel'
import { roundLabel, type RoundsRead } from './planRoundModel'
import { answeredWords, progressOld, viewOldReason } from './readAge'

/*
 * Work's "Needs cleanup" (R5:5.5-4): what was left stale and is no news to the round, moved off Work's first screen
 * onto Work → Activity, with one entry on the summary counting the same list. Two groups, each counted by the condition
 * the runtime already applies, never as a total less the rest:
 * - hosts the controller reports offline (GET /progress hosts[].reported_offline: silent past HostLostAfter, 30 s,
 *   active, not the external host, no stop or drain; store/presence.go), every host of the runtime;
 * - registered external work of the shown round, Running or Registered, with no heartbeat for over 60 s (plan-view
 *   external_work, every record of the round: the server's Unconfirmed, planviewsnapshot.go snapshotExternal, or its
 *   heartbeat age plus the time since the snapshot past 60 s, as Live's rows say it), that round only. Not working[]
 *   alone: a record on a task a runtime attempt now runs is only in that task's history and in external_work (review of
 *   16435ae7).
 * A group not read, or read old, is Unknown with the condition that made it so, and says no number for it. Old is each
 * read's own rule elsewhere on Work: GET /progress answered more than twice the shell's 5 s poll ago (the module page's
 * "Read as of", moduleDetailModel.ts isStale / SHELL_POLL_MS; PlanRoundNote's rounds the same); the plan view's
 * snapshot older than 60 s or not Fresh, or its refresh failed (the summary's Stale).
 * Claims: ~/agent-work/modview/WORK_V36_4_CLAIMS.md.
 */

/**
 * A group of the list: read whole, being read, or Unknown with why. An Unknown group still lists what the read named:
 * named now (a host reported offline while others may not be judged: counted, as "at least"), or as last read (an old
 * plan view: shown, never counted).
 */
export type CleanupGroup<T> = { state: 'ok'; items: T[] } | { state: 'reading' } | { state: 'unknown'; reason: string; items: T[]; lastRead?: boolean }
export type NeedsCleanup = { hosts: CleanupGroup<RuntimeHost>; work: CleanupGroup<ExternalWork>; root: string | null }

/**
 * Work on Live is Unconfirmed after 60 s without a heartbeat (plan-view freshness.external_work): the server's word, or
 * the heartbeat age it reported plus the time since its snapshot. One rule for Live's rows and this list.
 */
export const HEARTBEAT_UNCONFIRMED_S = 60
export function heartbeatUnconfirmed(work: { heartbeat_age_s: number; status?: string }, view: Pick<PlanSnapshot, 'as_of'>, now: number): boolean {
  const drift = Math.max(0, (now - Date.parse(view.as_of)) / 1000)
  return work.status === 'Unconfirmed' || work.heartbeat_age_s + drift > HEARTBEAT_UNCONFIRMED_S
}

/** GET /progress as the shell's cache holds it; dataUpdatedAt is when it last answered (0: never). */
type ProgressRead = { data: RuntimeProgress | undefined; error: unknown; isError: boolean; isPaused: boolean; dataUpdatedAt: number }
const failure = (q: { error: unknown; isError: boolean; isPaused: boolean }) =>
  q.isPaused ? 'offline' : q.isError ? (q.error instanceof Error ? q.error.message : 'Request failed') : ''

/** The hosts the controller reports offline, from GET /progress as the shell last read it. */
export function cleanupHosts(progress: ProgressRead, now: number): CleanupGroup<RuntimeHost> {
  const failed = failure(progress)
  // A failed or paused refresh voids the last answer, as the rest of the Console reads it (useOwnerYours current()).
  if (failed) return { state: 'unknown', reason: `GET /progress could not be read (${failed})`, items: [] }
  const p = progress.data
  if (p === undefined) return { state: 'reading' }
  const items = offlineHosts(p.hosts)
  // A refresh that hangs neither fails nor answers: the last answer is then old, and its hosts (none offline, too) are
  // listed as last read, never counted (platform review of b48fec12: a 2-minute-old "0 offline hosts" read as now).
  // Its age in seconds (review: "just now" for an answer 11–59 s old contradicted "older than twice the 5 s poll").
  if (progressOld(progress.dataUpdatedAt, now)) {
    return { state: 'unknown', reason: `GET /progress is older than twice the shell's ${SHELL_POLL_MS.progress / 1000} s poll: ${answeredWords(progress.dataUpdatedAt, now)}`, items, lastRead: true }
  }
  if (p.presence_unjudged) return { state: 'unknown', reason: `host presence cannot be judged since ${ago(p.presence_unjudged.since, now)}: ${p.presence_unjudged.why}`, items }
  if (p.presence_deferred) return { state: 'unknown', reason: `host presence judgment is deferred since ${ago(p.presence_deferred.since, now)}: ${p.presence_deferred.why}`, items }
  const unreported = p.hosts.filter(h => h.reported_offline === undefined).map(h => h.host_id)
  if (unreported.length) return { state: 'unknown', reason: `the platform does not say whether ${unreported.join(', ')} ${unreported.length === 1 ? 'is' : 'are'} offline (a platform before W-31 3.5)`, items }
  return { state: 'ok', items }
}

/** What plan view the external work is read from: its last answer, why it failed, the round and the rounds read. */
export type CleanupViewRead = { view: PlanSnapshot | undefined; failed: string | null; root: string | null; rounds: RoundsRead }

/** The states of registered external work that still expect a heartbeat (planviewsnapshot.go: working's own test). */
const OPEN_STATES: readonly string[] = ['Running', 'Registered']

/** The shown round's registered external work, still open, with no heartbeat for over 60 s; each record once. */
export function cleanupWork(read: CleanupViewRead, now: number): CleanupGroup<ExternalWork> {
  const { view, root } = read
  if (root === null) {
    if (read.rounds.state === 'reading') return { state: 'reading' }
    if (read.rounds.state === 'unknown') return { state: 'unknown', reason: `which round to read is Unknown (${read.rounds.reason})`, items: [] }
    if (read.rounds.newest !== null) return { state: 'reading' }
    // The list of rounds holds none: external work is registered to a round, so there is none to list; but only from a
    // list that is now (the same GET /progress, the same 10 s rule as the hosts).
    if (read.rounds.failed) return { state: 'unknown', reason: `which round to read is Unknown: GET /progress failed (${read.rounds.failed}) after it listed no round`, items: [] }
    if (progressOld(read.rounds.at, now)) return { state: 'unknown', reason: `which round to read is Unknown: GET /progress ${answeredWords(read.rounds.at, now)}, when it listed no round`, items: [] }
    return { state: 'ok', items: [] }
  }
  if (!view) return read.failed ? { state: 'unknown', reason: `${roundLabel(root)}'s plan view could not be read (${read.failed})`, items: [] } : { state: 'reading' }
  const seen = new Set<string>()
  const items: ExternalWork[] = []
  for (const w of view.external_work) {
    if (!OPEN_STATES.includes(w.state) || !heartbeatUnconfirmed(w, view, now) || seen.has(w.external_work_id)) continue
    seen.add(w.external_work_id)
    items.push(w)
  }
  const old = viewOldReason(view, root, read.failed, now)
  return old ? { state: 'unknown', reason: old, items, lastRead: true } : { state: 'ok', items }
}

export function needsCleanup(progress: ProgressRead, read: CleanupViewRead, now: number): NeedsCleanup {
  return { hosts: cleanupHosts(progress, now), work: cleanupWork(read, now), root: read.root }
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`
/** A group's count as its heading says it: "2", "1+ · Unknown" (named now, others not judged), "Unknown", "reading…". */
export const groupCount = (g: CleanupGroup<unknown>) => g.state === 'ok' ? String(g.items.length) : g.state === 'reading' ? 'reading…'
  : g.lastRead || g.items.length === 0 ? 'Unknown' : `${g.items.length}+ · Unknown`
const scopeOf = (c: NeedsCleanup) => c.root ? roundLabel(c.root) : 'the round'
/** The two groups' names as the list's headings say them (each followed by its count and its reach). */
export const HOSTS_GROUP = 'Hosts the controller reports offline'
export const WORK_GROUP = `Registered external work with no heartbeat for over ${HEARTBEAT_UNCONFIRMED_S} seconds`

/** Why each group is not whole, in the list's words. */
export function cleanupGaps(c: NeedsCleanup): string[] {
  return [
    ...(c.hosts.state === 'unknown' ? [`offline hosts Unknown: ${c.hosts.reason}`] : []),
    ...(c.work.state === 'unknown' ? [`${scopeOf(c)} external work Unknown: ${c.work.reason}`] : []),
  ]
}

/** The hosts' part of the words: every host of the runtime, so no round is named. */
function hostsWords(g: CleanupGroup<unknown>): string {
  if (g.state === 'ok') return plural(g.items.length, 'offline host', 'offline hosts')
  if (g.state === 'reading') return 'offline hosts reading…'
  // Named now while others are not judged: "at least"; nothing named: the count is Unknown, not 0.
  return g.items.length && !g.lastRead ? `${g.items.length}+ offline hosts, Unknown: ${g.reason}` : `offline hosts Unknown: ${g.reason}`
}

/** The external work's part: the shown round's only, and it says so in its own words. */
function workWords(c: NeedsCleanup): string {
  const g = c.work
  // A runtime that holds no round has no round's registered work to read.
  if (c.root === null && g.state === 'ok') return `${plural(g.items.length, 'external work', 'external works')} without heartbeat: the runtime holds no round`
  const where = `in ${scopeOf(c)}`
  if (g.state === 'ok') return `${plural(g.items.length, 'external work', 'external works')} without heartbeat ${where}`
  if (g.state === 'reading') return `external work ${where} reading…`
  // Rows of an old plan view are listed as last read, never counted.
  return `external work without heartbeat ${where} Unknown: ${g.reason}`
}

/**
 * The entry's and the list's head words, each group with its own count and reach (review of 16435ae7: one total of a
 * runtime-wide group and a one-round group hid the round, and work registered in another round could not be seen as
 * missing): "1 offline host · 2 external works without heartbeat in Round 5"; 0 as 0, never "all clean"; a group not
 * read whole says Unknown and why, in its own part; "reading…" in the part being read.
 */
export function cleanupWords(c: NeedsCleanup): string {
  return `${hostsWords(c.hosts)} · ${workWords(c)}`
}

/** Each group with its source and reach, for the entry's title, "How this is read" and the list's head. */
export function cleanupBreakdown(c: NeedsCleanup): string[] {
  return [
    `Hosts the controller reports offline ${groupCount(c.hosts)}: every host of the runtime (GET /progress, reported_offline: silent past 30 s, active, no stop or drain).`,
    `Registered external work with no heartbeat for over ${HEARTBEAT_UNCONFIRMED_S} s ${groupCount(c.work)}: ${scopeOf(c)} only (plan-view${c.root ? ` ${c.root}` : ''} external_work, Running or Registered, Unconfirmed; those on a task a runtime attempt runs included); each other round's is on its own Live tab, not read here.`,
    ...cleanupGaps(c).map(g => `Unknown: ${g}.`),
  ]
}
