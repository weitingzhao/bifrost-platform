import { describe, expect, it } from 'vitest'
import type { AgentThread } from '@/api/agentThreads'
import { GO_ZERO_DECIDED_AT } from '@/api/approvalsServerContract'
import { buildPlatformApproval } from '@/api/approvalsApiFixture'
import { needsYouBadge, needsYouCountText } from '@/pages/shell/needs-you/needsYouModel'
import {
  checkLines,
  groupOpenApprovals,
  recentlyHandled,
  recentlyHandledApprovals,
  waitingCount,
} from '@/pages/shell/needs-you/needsYouGroups'

const now = Date.UTC(2026, 9, 10, 18)

function at(msAgo: number): string {
  return new Date(now - msAgo).toISOString()
}

function local(msAgo: number): string {
  return new Date(now - msAgo).toLocaleString()
}

function thread(over: Partial<AgentThread> = {}): AgentThread {
  return {
    thread: 'thread-1',
    vendor: 'claude',
    host: 'mbp',
    event: 'turn_start',
    at: at(60_000),
    turn_started_at: at(120_000),
    status: 'in_turn',
    quiet_seconds: 0,
    in_turn_seconds: 10,
    threshold_seconds: 600,
    ...over,
  }
}

describe('needs you groups', () => {
  it('lists a pending drill apart and never counts it (W-31 2.5 gap 2, v4 §20.3 rule 8)', () => {
    const groups = groupOpenApprovals([
      buildPlatformApproval({ id: 'real' }),
      buildPlatformApproval({ id: 'drill', drill: true }),
      buildPlatformApproval({ id: 'old-drill', drill: true, expires_at: at(60_000) }),
      buildPlatformApproval({ id: 'closed-drill', drill: true, status: 'rejected' }),
    ], now)
    expect(groups.decide.map(i => i.id)).toEqual(['real'])
    expect(groups.drills.map(i => i.id)).toEqual(['drill'])
    expect(waitingCount(groups, 0)).toBe(1)
  })

  it('splits open approvals into decide, run, and check, and leaves the rest out', () => {
    const groups = groupOpenApprovals(
      [
        buildPlatformApproval({ id: 'pend', reason: 'Needs a decision.' }),
        buildPlatformApproval({
          id: 'expired-pending',
          reason: 'Too late.',
          expires_at: at(60_000),
        }),
        buildPlatformApproval({
          id: 'run',
          status: 'approved',
          runner: 'owner',
          reason: 'Publish the ConfigMap.',
          execution: null,
        }),
        buildPlatformApproval({
          id: 'ran',
          status: 'approved',
          runner: 'owner',
          reason: 'Already finished.',
          execution: { exit_code: 0, finished_at: at(60_000) },
        }),
        buildPlatformApproval({
          id: 'platform',
          status: 'approved',
          runner: 'platform',
          reason: 'Platform runs this itself.',
        }),
        buildPlatformApproval({
          id: 'running',
          status: 'running',
          runner: 'owner',
          reason: 'Already running.',
        }),
        buildPlatformApproval({
          id: 'check',
          status: 'unknown',
          reason: 'The lease lapsed.',
          error: 'lease expired before a result',
        }),
        buildPlatformApproval({
          id: 'check-empty',
          status: 'unknown',
          reason: 'No error text.',
          error: '  ',
        }),
      ],
      now,
    )

    expect(groups.decide.map(item => item.id)).toEqual(['pend'])
    expect(groups.terminal.map(item => item.id)).toEqual(['run'])
    expect(groups.check.map(item => item.id)).toEqual(['check', 'check-empty'])
    expect(checkLines('lease expired before a result')).toEqual([
      'The result could not be confirmed: lease expired before a result',
      'Open it and say what happened.',
    ])
    expect(checkLines('  ')[0]).toBe('The result could not be confirmed.')
    const total = waitingCount(groups, 2)
    expect(total).toBe(6)
    expect(needsYouBadge({ state: 'known', count: total })).toEqual({
      text: '6',
      title: 'Needs you: 6 waiting',
      tone: 'attention',
    })
    expect(needsYouCountText({ state: 'unknown', reason: 'approvals: boom' })).toBe('Unknown')
    expect(needsYouBadge({ state: 'unknown', reason: 'approvals: boom' })?.text).toBe('?')
  })
})

describe('recently handled', () => {
  const hour = 60 * 60_000

  it('writes one plain line per closed approval and drops anything older than a day', () => {
    const rows = recentlyHandledApprovals(
      [
        buildPlatformApproval({
          id: 'done',
          status: 'executed',
          number: 3,
          reason: 'Sync rocket.',
          decided_at: at(2 * hour),
          execution: { exit_code: 0, finished_at: at(hour), duration_ms: 1688 },
        }),
        buildPlatformApproval({
          id: 'broke',
          status: 'failed',
          number: 4,
          reason: 'Drain the node.',
          decided_at: at(3 * hour),
          execution: { exit_code: 7, finished_at: at(2 * hour) },
        }),
        buildPlatformApproval({
          id: 'no-code',
          status: 'failed',
          number: 5,
          reason: 'Failed without a code.',
          decided_at: at(4 * hour),
          execution: { finished_at: at(3 * hour) },
        }),
        buildPlatformApproval({
          id: 'nope',
          status: 'rejected',
          number: 6,
          reason: 'Do not sync.',
          decided_at: at(5 * hour),
        }),
        buildPlatformApproval({
          id: 'old-pass',
          status: 'expired',
          number: 7,
          reason: 'Sat too long.',
          decided_at: GO_ZERO_DECIDED_AT,
          expires_at: at(6 * hour),
        }),
        buildPlatformApproval({
          id: 'stale',
          status: 'executed',
          reason: 'Yesterday.',
          decided_at: at(25 * hour),
          execution: { exit_code: 0, finished_at: at(25 * hour) },
        }),
        buildPlatformApproval({
          id: 'still-open',
          status: 'pending',
          reason: 'Still waiting.',
        }),
        buildPlatformApproval({
          id: 'edge',
          status: 'rejected',
          reason: 'Exactly a day.',
          decided_at: at(24 * hour),
        }),
        buildPlatformApproval({
          id: 'just-over',
          status: 'rejected',
          reason: 'A day and a bit.',
          decided_at: new Date(now - 24 * hour - 1).toISOString(),
        }),
      ],
      now,
    )

    expect(rows.map(row => (row.kind === 'approval' ? row.item.id : ''))).toEqual([
      'done',
      'broke',
      'no-code',
      'nope',
      'old-pass',
      'edge',
    ])
    expect(rows.map(row => row.line)).toEqual([
      `You approved it; it ran and finished at ${local(hour)}`,
      'You approved it; it ran and failed (exit code 7)',
      'You approved it; it ran and failed.',
      `Rejected at ${local(5 * hour)}`,
      `Expired at ${local(6 * hour)}`,
      `Rejected at ${local(24 * hour)}`,
    ])
    expect(rows.some(row => row.line.includes('0001'))).toBe(false)
  })

  it('reads a resolved record as closed by the Owner', () => {
    const rows = recentlyHandledApprovals(
      [
        buildPlatformApproval({
          id: 'did-not',
          status: 'failed',
          reason: 'The lease lapsed.',
          decided_at: at(2 * hour),
          resolution: { outcome: 'not_run', note: 'asleep', at: at(hour), by: 'owner' },
        }),
        buildPlatformApproval({
          id: 'did-ok',
          status: 'executed',
          reason: 'It ran.',
          decided_at: at(3 * hour),
          resolution: { outcome: 'succeeded', note: 'finished', at: at(2 * hour), by: 'owner' },
        }),
        buildPlatformApproval({
          id: 'did-bad',
          status: 'failed',
          reason: 'It failed.',
          decided_at: at(4 * hour),
          resolution: { outcome: 'failed', note: 'broke', at: at(3 * hour), by: 'owner' },
        }),
      ],
      now,
    )
    expect(rows.map(row => row.line)).toEqual([
      'You closed it: it did not run',
      'You closed it: it ran and succeeded',
      'You closed it: it ran and failed',
    ])
  })

  it('keeps the ten newest closed approvals', () => {
    const items = Array.from({ length: 12 }, (_, i) =>
      buildPlatformApproval({
        id: `ap-${i}`,
        status: 'executed',
        number: i + 1,
        reason: `Request ${i}`,
        decided_at: at((12 - i) * 60_000),
        execution: { exit_code: 0, finished_at: at((12 - i) * 60_000) },
      }),
    )
    const rows = recentlyHandledApprovals(items, now)
    expect(rows.map(row => (row.kind === 'approval' ? row.item.id : ''))).toEqual([
      'ap-11',
      'ap-10',
      'ap-9',
      'ap-8',
      'ap-7',
      'ap-6',
      'ap-5',
      'ap-4',
      'ap-3',
      'ap-2',
    ])
  })

  it('says a notified thread is active again, or that its turn ended', () => {
    const rows = recentlyHandled(
      [],
      [
        thread({
          thread: 'busy',
          title: 'Heartbeat work',
          status: 'in_turn',
          event: 'turn_start',
          notified_at: at(30 * 60_000),
        }),
        thread({
          thread: 'done',
          title: 'Codex job',
          status: 'idle',
          event: 'turn_end',
          notified_at: at(hour),
        }),
        thread({
          thread: 'still-quiet',
          title: 'Still silent',
          status: 'silent',
          event: 'before_tool',
          notified_at: at(hour),
        }),
        thread({
          thread: 'no-name',
          title: '  ',
          status: 'waiting_owner',
          event: 'waiting_owner',
          notified_at: at(2 * hour),
        }),
        thread({
          thread: 'ancient',
          title: 'Too old',
          status: 'idle',
          event: 'turn_end',
          notified_at: at(25 * hour),
        }),
        thread({
          thread: 'never',
          title: 'No push',
          status: 'in_turn',
          notified_at: GO_ZERO_DECIDED_AT,
        }),
      ],
      now,
    )
    expect(rows.map(row => row.line)).toEqual([
      'Heartbeat work was silent; it is active again',
      'Codex job was silent; its turn ended',
      'no-name was silent; it is active again',
    ])
  })
})
