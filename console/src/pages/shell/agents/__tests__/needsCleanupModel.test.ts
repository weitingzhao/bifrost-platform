import { describe, expect, it } from 'vitest'
import prod from '@/api/__fixtures__/plan-view-pv2-prod-r4.json'
import presenceFixture from '@/api/__fixtures__/agentrt-progress-presence.testdb.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parsePlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import type { RoundsRead } from '../planRoundModel'
import { cleanupBreakdown, cleanupGaps, cleanupWork, cleanupWords, groupCount, heartbeatUnconfirmed, needsCleanup, type NeedsCleanup } from '../needsCleanupModel'

/*
 * R5:5.5-4: what was left stale on Work's first screen (PROD 2026-11-01: "Host mbp-dev is offline: its last poll was 14
 * days ago."; two registered external works, TD-457 and TD-459, 50 hours without a heartbeat) is one list on Work →
 * Activity, counted by the runtime's own rules, and one entry on the summary counting that same list. Claims and sources:
 * the R5:5.5-4 hand-in report.
 */

/** GET /progress from real rows (W-31 3.5): h1 online, h2 reported offline 45 s after its last poll. */
const presence = parseRuntimeProgress(presenceFixture)
const at = Date.parse(presence.generated_at)

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
  const list = (o: Partial<NeedsCleanup>): NeedsCleanup => ({ ...needsCleanup(read(view(now)), now), ...o })
  // Review of 16435ae7: the count names its round in visible words. The offline-hosts group that stood beside it went
  // with the dispatch lane (2026-11-14).
  it('says the count and its reach, the round in visible words; 0 is said as 0, never "all clean"', () => {
    const c = needsCleanup(read(view(now)), now)
    expect(cleanupWords(c)).toBe('1 external work without heartbeat in Round 4')
    expect(cleanupBreakdown(c)).toEqual([
      "Registered external work with no heartbeat for over 60 s 1: Round 4 only (plan-view R4 external_work, Running or Registered, Unconfirmed; those on a task a runtime attempt runs included); each other round's is on its own Live tab, not read here.",
    ])
    expect(cleanupWords(list({ work: { state: 'ok', items: [] } }))).toBe('0 external works without heartbeat in Round 4')
    expect(cleanupWords({ ...list({}), root: null, work: { state: 'ok', items: [] } })).toBe('0 external works without heartbeat: the runtime holds no round')
  })
  it('says what is Unknown and why, never counting a last read', () => {
    const old = needsCleanup(read(view(now - 90_000)), now)
    expect(cleanupWords(old)).toMatch(/^external work without heartbeat in Round 4 Unknown: Round 4's plan view is older than 60 s; as of /)
    expect(groupCount(old.work)).toBe('Unknown')
    const none = needsCleanup(read(undefined, { failed: 'down' }), now)
    expect(cleanupWords(none)).toBe("external work without heartbeat in Round 4 Unknown: Round 4's plan view could not be read (down)")
    expect(cleanupGaps(none)).toHaveLength(1)
    expect(cleanupWords(needsCleanup(read(undefined), now))).toBe('external work in Round 4 reading…')
  })
})
