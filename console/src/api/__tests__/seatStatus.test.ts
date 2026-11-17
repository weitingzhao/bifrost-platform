import { describe, expect, it } from 'vitest'
import type { AgentThread } from '@/api/agentThreads'
import type { ApprovalItem } from '@/api/approvals'
import type { LineageThread } from '@/api/lineage'
import { linesOf, sessionOf, stageOf, type SeatStatus } from '@/api/seatStatus'

// 2026-11-17 (the Owner: he asks for progress because Ops does not show it):
// one row per line from the seat's own status, beside what the platform
// counts — its cards for the Owner (drills apart), its commits by the
// furthest stage, its session heartbeat. A line that waits comes first.
describe('lines', () => {
  const now = new Date('2026-11-17T16:00:00Z')
  const status = (seat: string, at: string, waiting_on?: string): SeatStatus => ({
    seat, message_id: 'm_1', at, goal: 'g', doing: 'd', percent: 40, percent_basis: '2 of 5', next: 'n', ...(waiting_on ? { waiting_on } : {}),
  })
  it('joins status, heartbeat, cards and commits by session', () => {
    const statuses = [status('thread:local_a', '2026-11-17T15:50:00Z'), status('thread:local_b', '2026-11-17T13:00:00Z', 'the window')]
    const threads = [{ thread: 'local_a', status: 'in_turn', quiet_seconds: 5, at: '2026-11-17T15:59:00Z', title: 'A' }] as AgentThread[]
    const approvals = [
      { requester: 'local_a', drill: false }, { requester: 'local_a', drill: true }, { requester: 'local_c', drill: false },
    ] as ApprovalItem[]
    const lineage = [{ session: 'local_a', title: 'Line A', repos: [{ repo: 'p', commits: [
      { landed: false }, { landed: true }, { landed: true, reached: [{ env: 'stg' }] }, { landed: true, reached: [{ env: 'stg' }, { env: 'prod' }] },
    ] }] }] as unknown as LineageThread[]
    const rows = linesOf(statuses, threads, approvals, lineage, now)
    expect(rows.filter(r => r.status).map(r => r.session)).toEqual(['local_a', 'local_b'])
    const a = rows[0]
    expect(a.title).toBe('Line A')
    expect(a.ownerCards).toBe(1)
    expect(a.commits).toEqual({ written: 1, main: 1, stg: 1, prod: 1 })
    expect(a.thread?.status).toBe('in_turn')
    expect(a.statusAgeMinutes).toBe(10)
    const b = rows[1]
    expect(b.thread).toBeUndefined()
    expect(b.statusAgeMinutes).toBe(180)
  })
  it('puts a waiting line first, then the newest status', () => {
    const rows = linesOf([status('thread:local_new', '2026-11-17T15:59:00Z'), status('thread:local_wait', '2026-11-17T10:00:00Z', 'Owner')], [], [], [], now)
    expect(rows.map(r => r.session)).toEqual(['local_wait', 'local_new'])
  })
  it('keeps a malformed status as a row, with its problem', () => {
    const bad = { seat: 'thread:local_bad', message_id: 'm_2', at: '2026-11-17T15:00:00Z', summary: 'hi', problem: 'format: no next',
      goal: '', doing: '', percent: 0, percent_basis: '', next: '' }
    const rows = linesOf([bad], [], [], [], now)
    expect(rows[0].status?.problem).toBe('format: no next')
  })
  // 2026-11-17 (the coordinator): with only one line reporting, the page read
  // as one line working. A line active in the last day (a heartbeat or a
  // commit) without a status gets a row of its own, after those that report;
  // one quiet for longer does not.
  it('lists active lines that have not reported, after those that have', () => {
    const threads = [
      { thread: 'local_quiet', session: 'local_quiet', status: 'idle', quiet_seconds: 60, at: '2026-11-17T15:30:00Z' },
      { thread: 'local_old', session: 'local_old', status: 'idle', quiet_seconds: 9e4, at: '2026-11-15T10:00:00Z' },
    ] as AgentThread[]
    const lineage = [{ session: 'local_committer', title: 'CI', last_at: '2026-11-17T14:00:00Z', repos: [{ repo: 'p', commits: [{ landed: true }] }] }] as unknown as LineageThread[]
    const rows = linesOf([status('thread:local_a', '2026-11-17T15:50:00Z')], threads, [], lineage, now)
    expect(rows.map(r => [r.session, r.status ? 'reports' : 'not yet'])).toEqual([
      ['local_a', 'reports'], ['local_quiet', 'not yet'], ['local_committer', 'not yet'],
    ])
    expect(rows[2].title).toBe('CI')
    expect(rows[2].commits.main).toBe(1)
  })
  it('reads sessions and stages', () => {
    expect(sessionOf('thread:local_x')).toBe('local_x')
    expect(sessionOf('seat:ci')).toBe('seat:ci')
    expect(stageOf({ landed: true, reached: [{ env: 'PROD' }] })).toBe('prod')
    expect(stageOf({ landed: false })).toBe('written')
  })
})
