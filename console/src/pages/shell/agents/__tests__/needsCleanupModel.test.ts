import { describe, expect, it } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import presenceFixture from '@/api/__fixtures__/agentrt-progress-presence.testdb.json'
import stgProgress from '@/api/__fixtures__/agentrt-progress.stg.json'
import { parseRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import type { RoundsRead } from '../planRoundModel'
import { cleanupBreakdown, cleanupGaps, cleanupHosts, cleanupWork, cleanupWords, groupCount, heartbeatUnconfirmed, needsCleanup, type NeedsCleanup } from '../needsCleanupModel'

/*
 * R5:5.5-4: what was left stale on Work's first screen (PROD 2026-11-01: "Host mbp-dev is offline: its last poll was 14
 * days ago."; two registered external works, TD-457 and TD-459, 50 hours without a heartbeat) is one list on Work →
 * Activity, counted by the runtime's own rules, and one entry on the summary counting that same list. Claims and sources:
 * the R5:5.5-4 hand-in report.
 */

/** GET /progress from real rows (W-31 3.5): h1 online, h2 reported offline 45 s after its last poll. */
const presence = parseRuntimeProgress(presenceFixture)
const at = Date.parse(presence.generated_at)
/** GET /progress as the shell's cache holds it, answered `age` ms before `at` (fresh by default). */
const ok = (data: RuntimeProgress, age = 0) => ({ data, error: null, isError: false, isPaused: false, dataUpdatedAt: at - age })

/**
 * The PROD R4 plan view (2026-10-27) as of `now`, with two registered external works Running: one silent, one beating;
 * both in working (no runtime attempt on their task) unless underAttempt: then only in external_work, as the server
 * leaves a record on a task a live attempt runs (planviewsnapshot.go: working only when live[task] == nil).
 */
function view(now: number, o: { silentS?: number; beatingS?: number; serverSays?: 'Unconfirmed' | 'Running'; database?: string; underAttempt?: boolean } = {}): PlanSnapshot {
  const raw = structuredClone(prod) as unknown as { as_of: string; working: unknown[]; external_work: Record<string, unknown>[]; freshness: { database: { status: string } } }
  const base = raw.external_work[0]
  const running = (id: string, age: number, status: string) => ({ ...base, external_work_id: id, source_run_key: id, state: 'Running', type: 'heartbeat', status,
    heartbeat_age_s: age, heartbeat_at: new Date(now - age * 1000).toISOString(), ended_at: undefined, exit_code: undefined, report_digest: undefined, report_path_digest: undefined })
  const silent = o.silentS ?? 50 * 3600
  const work = [running('td-457', silent, o.serverSays ?? (silent > 60 ? 'Unconfirmed' : 'Running')), running('beating', o.beatingS ?? 5, 'Running')]
  Object.assign(raw, { as_of: new Date(now).toISOString(), working: o.underAttempt ? [] : work, external_work: [...raw.external_work, ...work] })
  raw.freshness.database.status = o.database ?? 'Fresh'
  return parsePlanSnapshot(raw, 'R4')
}
const roundsOk: RoundsRead = { state: 'ok', rounds: [{ id: 'R4', title: '' }], newest: 'R4', ignored: [], at: 0, failed: '' }
const read = (v: PlanSnapshot | undefined, o: { failed?: string | null; root?: string | null; rounds?: RoundsRead } = {}) =>
  ({ view: v, failed: o.failed ?? null, root: o.root === undefined ? 'R4' : o.root, rounds: o.rounds ?? roundsOk })

describe('the offline hosts', () => {
  it('are the hosts the controller reports offline, every one, in For you\'s order', () => {
    expect(cleanupHosts(ok(presence), at)).toEqual({ state: 'ok', items: [presence.hosts[1]] })
    expect(cleanupHosts(ok({ ...presence, hosts: presence.hosts.map(h => ({ ...h, reported_offline: false })) }), at)).toEqual({ state: 'ok', items: [] })
  })
  it('are Unknown, with the condition, when GET /progress is not read or presence is not judged; reading before any answer', () => {
    expect(cleanupHosts({ data: presence, error: new Error('Agent runtime: 503'), isError: true, isPaused: false, dataUpdatedAt: 0 }, at))
      .toEqual({ state: 'unknown', reason: 'GET /progress could not be read (Agent runtime: 503)', items: [] })
    expect(cleanupHosts({ data: undefined, error: null, isError: false, isPaused: true, dataUpdatedAt: 0 }, at)).toMatchObject({ state: 'unknown', reason: 'GET /progress could not be read (offline)' })
    expect(cleanupHosts({ data: undefined, error: null, isError: false, isPaused: false, dataUpdatedAt: 0 }, at)).toEqual({ state: 'reading' })
    const since = new Date(at - 3 * 60_000).toISOString()
    const unjudged = cleanupHosts(ok({ ...presence, presence_unjudged: { since, why: 'the platform has not answered its health probe' } }), at)
    expect(unjudged).toEqual({ state: 'unknown', reason: 'host presence cannot be judged since 3 min ago: the platform has not answered its health probe', items: [presence.hosts[1]] })
    expect(cleanupHosts(ok({ ...presence, presence_deferred: { since, why: 'h1 silent for up to 2m5s but not judged' } }), at))
      .toMatchObject({ state: 'unknown', reason: 'host presence judgment is deferred since 3 min ago: h1 silent for up to 2m5s but not judged' })
    // A platform before W-31 3.5 does not say which hosts are offline: not "none".
    expect(cleanupHosts(ok(parseRuntimeProgress(stgProgress)), at))
      .toEqual({ state: 'unknown', reason: 'the platform does not say whether gate-b, mbp-dev are offline (a platform before W-31 3.5)', items: [] })
  })
})

// Platform review of b48fec12: a GET /progress refresh that hangs neither fails nor answers, and its two-minute-old
// "0 offline hosts" was said as now. Old is the module page's rule for this read: more than twice the shell's 5 s poll.
describe('the offline hosts from an old GET /progress', () => {
  const allOnline = { ...presence, hosts: presence.hosts.map(h => ({ ...h, reported_offline: false })) }
  it('are Unknown, with the answer\'s age, when it is two minutes old: no number, not "0 offline hosts"', () => {
    const old = cleanupHosts(ok(allOnline, 120_000), at)
    expect(old).toEqual({ state: 'unknown', lastRead: true, items: [],
      reason: `GET /progress is older than twice the shell's 5 s poll: last answered 2 min ago (${new Date(at - 120_000).toISOString()})` })
    expect(groupCount(old)).toBe('Unknown')
    const words = cleanupWords({ hosts: old, work: { state: 'ok', items: [] }, root: 'R4' })
    expect(words).toBe(`offline hosts Unknown: GET /progress is older than twice the shell's 5 s poll: last answered 2 min ago (${new Date(at - 120_000).toISOString()}) · 0 external works without heartbeat in Round 4`)
    expect(words).not.toMatch(/\d+\+? offline hosts?/)
  })
  it('lists an old answer\'s offline hosts as last read, never counted; fresh within twice the poll', () => {
    const old = cleanupHosts(ok(presence, 11_000), at)
    expect(old).toMatchObject({ state: 'unknown', lastRead: true, items: [presence.hosts[1]] })
    expect(groupCount(old)).toBe('Unknown')
    // In seconds (review: "just now" for an answer 11 s old contradicted "older than twice the 5 s poll").
    expect(cleanupWords({ hosts: old, work: { state: 'ok', items: [] }, root: 'R4' })).toBe(
      `offline hosts Unknown: GET /progress is older than twice the shell's 5 s poll: last answered 11 s ago (${new Date(at - 11_000).toISOString()}) · 0 external works without heartbeat in Round 4`)
    expect(cleanupHosts(ok(presence, 10_000), at)).toEqual({ state: 'ok', items: [presence.hosts[1]] })
  })
})

describe('the registered external work with no heartbeat', () => {
  const now = Date.parse('2026-11-01T10:00:00Z')
  it('is the round\'s registered work Unconfirmed as Live says it: the server\'s word, or its heartbeat age plus the time since', () => {
    expect(cleanupWork(read(view(now)), now)).toMatchObject({ state: 'ok', items: [{ external_work_id: 'td-457' }] })
    // 50 s at the snapshot, 15 s since: past 60 s now, as Live's row says it.
    const v = view(now - 15_000, { silentS: 50, serverSays: 'Running' })
    expect(heartbeatUnconfirmed(v.working[0] as { heartbeat_age_s: number }, v, now)).toBe(true)
    expect(cleanupWork(read(v), now)).toMatchObject({ state: 'ok', items: [{ external_work_id: 'td-457' }] })
    expect(cleanupWork(read(view(now, { silentS: 20 })), now)).toEqual({ state: 'ok', items: [] })
  })
  // Review of 16435ae7: working[] alone missed a record on a task a runtime attempt runs, and said "None" of it.
  it('takes every open record of the round\'s external_work, those under a running attempt too; never Submitted or Cancelled; each once', () => {
    expect(cleanupWork(read(view(now, { underAttempt: true })), now)).toMatchObject({ state: 'ok', items: [{ external_work_id: 'td-457' }] })
    const v = view(now)
    const ended = (state: string) => ({ ...v.external_work[0], external_work_id: `ended-${state}`, state, status: state, heartbeat_age_s: 50 * 3600 })
    const withEnded = { ...v, external_work: [...v.external_work, ended('Submitted'), ended('Cancelled'), { ...v.external_work.at(-2)!, state: 'Registered' as const }] } as PlanSnapshot
    // The fixture's own four records are Submitted, with heartbeats days old: not open, not listed. td-457 given twice
    // (the second Registered) is one row.
    expect(cleanupWork(read(withEnded), now)).toMatchObject({ state: 'ok', items: [{ external_work_id: 'td-457' }] })
  })
  it('is Unknown when the round\'s plan view is not read, old or not Fresh; lists the last read as such, never counts it', () => {
    expect(cleanupWork(read(undefined, { failed: 'Plan view: 503' }), now)).toEqual({ state: 'unknown', reason: "Round 4's plan view could not be read (Plan view: 503)", items: [] })
    expect(cleanupWork(read(undefined), now)).toEqual({ state: 'reading' })
    // 90 s old: the one that was beating is past 60 s too by now, so both are listed, as last read, and neither counted.
    const old = cleanupWork(read(view(now - 90_000)), now)
    expect(old).toMatchObject({ state: 'unknown', lastRead: true, items: [{ external_work_id: 'td-457' }, { external_work_id: 'beating' }] })
    expect(old.state === 'unknown' && old.reason).toMatch(/^Round 4's plan view is older than 60 s; as of /)
    expect(cleanupWork(read(view(now), { failed: 'offline' }), now)).toMatchObject({ state: 'unknown', reason: expect.stringMatching(/^Round 4's plan view could not be read \(offline\)/) })
    expect(cleanupWork(read(view(now, { database: 'Stale' })), now)).toMatchObject({ state: 'unknown', reason: expect.stringMatching(/reads the database as Stale/) })
  })
  it('follows which round is read: reading, Unknown with why, or none to list when the runtime holds no round', () => {
    expect(cleanupWork(read(undefined, { root: null, rounds: { state: 'reading' } }), now)).toEqual({ state: 'reading' })
    expect(cleanupWork(read(undefined, { root: null, rounds: { state: 'unknown', reason: 'Offline' } }), now))
      .toEqual({ state: 'unknown', reason: 'which round to read is Unknown (Offline)', items: [] })
    expect(cleanupWork(read(undefined, { root: null, rounds: { ...roundsOk, rounds: [], newest: null, at: now } }), now)).toEqual({ state: 'ok', items: [] })
    // "No round" only from a round list that is now: the same GET /progress, the same 10 s rule as the hosts.
    expect(cleanupWork(read(undefined, { root: null, rounds: { ...roundsOk, rounds: [], newest: null, at: now - 31_000 } }), now)).toEqual({ state: 'unknown', items: [],
      reason: `which round to read is Unknown: GET /progress last answered 31 s ago (${new Date(now - 31_000).toISOString()}), when it listed no round` })
    expect(cleanupWork(read(undefined, { root: null, rounds: { ...roundsOk, rounds: [], newest: null, at: now, failed: 'down' } }), now))
      .toMatchObject({ state: 'unknown', reason: 'which round to read is Unknown: GET /progress failed (down) after it listed no round' })
  })
})

describe('the count of the list', () => {
  const now = at
  const list = (o: Partial<NeedsCleanup>): NeedsCleanup => ({ ...needsCleanup(ok(presence), read(view(now)), now), ...o })
  // Review of 16435ae7: one total ("3 items") of a runtime-wide group and a one-round group hid the round.
  it('says each group with its own count and reach, the round in visible words; 0 is said as 0, never "all clean"', () => {
    const c = needsCleanup(ok(presence), read(view(now)), now)
    expect(cleanupWords(c)).toBe('1 offline host · 1 external work without heartbeat in Round 4')
    expect(cleanupBreakdown(c)).toEqual([
      'Hosts the controller reports offline 1: every host of the runtime (GET /progress, reported_offline: silent past 30 s, active, no stop or drain).',
      "Registered external work with no heartbeat for over 60 s 1: Round 4 only (plan-view R4 external_work, Running or Registered, Unconfirmed; those on a task a runtime attempt runs included); each other round's is on its own Live tab, not read here.",
    ])
    expect(cleanupWords(list({ hosts: { state: 'ok', items: [] }, work: { state: 'ok', items: [] } }))).toBe('0 offline hosts · 0 external works without heartbeat in Round 4')
    expect(cleanupWords(list({ work: { state: 'ok', items: [] } }))).toBe('1 offline host · 0 external works without heartbeat in Round 4')
    expect(cleanupWords({ ...list({}), root: null, work: { state: 'ok', items: [] } })).toBe('1 offline host · 0 external works without heartbeat: the runtime holds no round')
  })
  it('says in its own part what of a group is Unknown and why, counting only what was read now ("n+"), never a last read', () => {
    const old = needsCleanup(ok(presence), read(view(now - 90_000)), now)
    expect(cleanupWords(old)).toMatch(/^1 offline host · external work without heartbeat in Round 4 Unknown: Round 4's plan view is older than 60 s; as of /)
    expect(groupCount(old.work)).toBe('Unknown')
    const deferred = { ...presence, presence_deferred: { since: presence.generated_at, why: 'probe failing' } }
    const partly = needsCleanup(ok(deferred), read(view(now)), now)
    expect(groupCount(partly.hosts)).toBe('1+ · Unknown')
    expect(cleanupWords(partly)).toBe('1+ offline hosts, Unknown: host presence judgment is deferred since just now: probe failing · 1 external work without heartbeat in Round 4')
    const none = needsCleanup({ data: undefined, error: new Error('down'), isError: true, isPaused: false, dataUpdatedAt: 0 }, read(undefined, { failed: 'down' }), now)
    expect(cleanupWords(none)).toBe("offline hosts Unknown: GET /progress could not be read (down) · external work without heartbeat in Round 4 Unknown: Round 4's plan view could not be read (down)")
    expect(cleanupGaps(none)).toHaveLength(2)
    expect(groupCount(none.hosts)).toBe('Unknown')
    expect(cleanupWords(needsCleanup({ data: undefined, error: null, isError: false, isPaused: false, dataUpdatedAt: 0 }, read(view(now)), now))).toBe('offline hosts reading… · 1 external work without heartbeat in Round 4')
    expect(cleanupWords(needsCleanup(ok(presence), read(undefined), now))).toBe('1 offline host · external work in Round 4 reading…')
  })
})
