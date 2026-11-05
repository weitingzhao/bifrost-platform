import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import planView from '@/api/__fixtures__/plan-view-collector.generated.json'
import sessions from '@/api/__fixtures__/agent-threads-sessions.generated.json'
import threads from '@/api/__fixtures__/agent-threads.generated.json'
import type { AgentThread, AgentThreadsResponse } from '@/api/agentThreads'
import { parsePlanSnapshot } from '@/api/planSnapshot'
import { PlanLiveWork, UnlinkedSessions } from '../PlanLiveWork'
import { compareInstant, coverageGroups, latestBySession, unlinkedSessions, type SessionsRead } from '../unlinkedSessionsModel'

// R5:5.5-12 §3.3, S1-3: Work → Live's Unlinked sessions and its Coverage line. The answers are Go's own
// (agentthreads TestAgentThreadsResponseFixture, store TestPlanViewCollectorWorkFixture, byte for byte); the plan view
// carries the session counts as the API adds them (agentrtapi sessionCoverage: coverage.sessions).

afterEach(cleanup)
const data = threads as unknown as AgentThreadsResponse
const view = () => parsePlanSnapshot({ ...planView, coverage: { ...planView.coverage, sessions } }, 'R4')
const ok = (res: AgentThreadsResponse = data): SessionsRead => ({ state: 'ok', data: res, stale: false })
function thread(over: Partial<AgentThread>): AgentThread {
  return { thread: 'x', vendor: 'claude', host: 'h1', event: 'turn_start', at: '2026-10-26T21:55:00Z', turn_started_at: '2026-10-26T21:55:00Z',
    status: 'in_turn', quiet_seconds: 1, in_turn_seconds: 1, threshold_seconds: 600, ...over }
}
const keys = (list: HTMLElement) => within(list).getAllByRole('listitem').map(li => li.getAttribute('data-session'))

describe('the Unlinked sessions of a P-c2 answer', () => {
  it('renders Live, Finished today and Unlinked by their stable identities', () => {
    render(<PlanLiveWork view={view()} stale={false} sessions={ok()} />)
    const now = screen.getByRole('list', { name: 'Work now' })
    expect(within(now).getAllByRole('listitem').map(li => li.textContent)).toEqual([
      expect.stringContaining('collector hook work'), expect.stringContaining('an MCP registration'), expect.stringContaining('a task with a live attempt'),
    ])
    expect(screen.getByText(/Finished today · 1/).closest('details')).toHaveTextContent('collector work submitted today')
    const unlinked = screen.getByRole('list', { name: 'Unlinked sessions' })
    expect(keys(unlinked)).toEqual([
      'h2/cursor/f3a1c2d4-0000-0000-0000-000000000000/0', 'h1/claude/6c7d8e9f-1111-2222-3333-444455556666/1', 'h1/claude/5a6b7c8d-1111-2222-3333-444455556666/1',
    ])
    const [old, refused, started] = within(unlinked).getAllByRole('listitem')
    expect(old).toHaveTextContent('cursor · h2 · f3a1c2d4'); expect(old).toHaveTextContent('unlinked · idle'); expect(old).toHaveTextContent('First seen · 2026-10-26T21:57:00Z · Last event · turn_end · No registration recorded')
    expect(refused).toHaveTextContent('claude · h1 · 6c7d8e9f · epoch 1'); expect(refused).toHaveTextContent('refused · in_turn')
    expect(refused).toHaveTextContent('Refused · Plan step refused'); expect(refused).toHaveTextContent('Tool · Bash')
    expect(started).toHaveTextContent('Last event · session_start · Client says · No plan context')
    expect(screen.getByRole('region', { name: 'Unlinked sessions' })).toHaveTextContent('Unlinked sessions · 3')
    expect(screen.getByText(/Codex hook threads · 1/)).toBeInTheDocument()
    const live = screen.getByRole('region', { name: 'Live work' })
    expect(live).toHaveTextContent('Sessions · 3 linked · 2 unlinked · 1 refused · binding rate 50%')
    expect(live).toHaveTextContent('Not judged · 1 unsupported · 0 unknown')
    // The three linked sessions are Live's, not Unlinked's; no title is shown.
    expect(within(unlinked).queryByText(/0b8e2f4a|7c1d9e3b|9e5f0a1c/)).toBeNull()
  })
  it('places the section after Finished today and before the Coverage line', () => {
    render(<PlanLiveWork view={view()} stale={false} sessions={ok()} />)
    const live = screen.getByRole('region', { name: 'Live work' })
    const text = live.textContent!
    expect(text.indexOf('Finished today')).toBeLessThan(text.indexOf('Unlinked sessions'))
    expect(text.indexOf('Unlinked sessions')).toBeLessThan(text.indexOf('Coverage ·'))
  })
})

describe('the five states, one row per identity', () => {
  it('lists unlinked, refused and unknown; counts unsupported; leaves linked to Live', () => {
    const res = { ...data, threads: [
      thread({ thread: 'l', session: 'l', epoch: 1, binding: { state: 'linked', external_work_id: 'ew_l' } }),
      thread({ thread: 'u', session: 'u', epoch: 1, binding: { state: 'unlinked' } }),
      thread({ thread: 'r', session: 'r', epoch: 1, binding: { state: 'refused' }, client_bind: { state: 'refused', reason: 'not_dev_tree' } }),
      thread({ thread: 'c', vendor: 'codex', session: 'c', epoch: 1, binding: { state: 'unsupported' }, server_note: 'codex_binding_after_s1_0' }),
      thread({ thread: 'k', session: 'k', epoch: 1, binding: { state: 'unknown' }, server_note: 'legacy_no_host' }),
    ] }
    const out = unlinkedSessions(res)
    expect(out.rows.map(r => [r.session, r.state, r.reason])).toEqual([
      ['k', 'unknown', 'A record from before the host was written'], ['r', 'refused', 'Refused · Not a dev plan'], ['u', 'unlinked', 'No registration recorded'],
    ])
    expect(out.unsupported).toBe(1)
    expect(out.runtimeUnreadable).toBe(false)
  })
  it('takes a key recovered under a new thread id as the same session, its latest row standing for it', () => {
    const res = [thread({ thread: 's', session: 's', epoch: 1, at: '2026-10-26T21:50:00Z', event: 'turn_end' }),
      thread({ thread: 's:1', session: 's', epoch: 1, at: '2026-10-26T21:58:00Z', event: 'before_tool' })]
    expect(latestBySession(res).map(t => t.thread)).toEqual(['s:1'])
    expect(unlinkedSessions({ ...data, threads: res.map(t => ({ ...t, binding: { state: 'unlinked' } })) }).rows.map(r => r.event)).toEqual(['before_tool'])
    // Another vendor with the same id, or another epoch, is another session.
    expect(latestBySession([...res, thread({ thread: 's', vendor: 'cursor', session: 's', epoch: 1 }), thread({ thread: 's', session: 's', epoch: 2 })])).toHaveLength(3)
  })
})

describe('the section when there is nothing to list, or nothing read', () => {
  it('says there are none', () => {
    render(<UnlinkedSessions read={ok({ ...data, threads: data.threads.filter(t => t.binding?.state === 'linked') })} />)
    expect(screen.getByRole('region', { name: 'Unlinked sessions' })).toHaveTextContent('Unlinked sessions · 0')
    expect(screen.getByText('No unlinked sessions in the threads retained.')).toBeInTheDocument()
  })
  it('says the thread list could not be read, and counts nothing', () => {
    render(<UnlinkedSessions read={{ state: 'failed', error: 'Agent threads: 503' }} />)
    expect(screen.getByRole('status')).toHaveTextContent('The thread list could not be read (Agent threads: 503)')
    expect(screen.getByRole('region', { name: 'Unlinked sessions' })).toHaveTextContent('Unlinked sessions · Unknown')
  })
  it('says the runtime could not be read when the bindings are Unknown for it', () => {
    render(<UnlinkedSessions read={ok({ ...data, threads: [thread({ thread: 'a', binding: { state: 'unknown' }, server_note: 'runtime_unreadable' })] })} />)
    expect(screen.getByRole('status')).toHaveTextContent("The runtime could not be read: these sessions' bindings are Unknown, not unlinked.")
    expect(screen.getByRole('listitem')).toHaveTextContent('unknown · in_turn')
  })
  it('says it is reading, and says a list as last read', () => {
    const { rerender } = render(<UnlinkedSessions read={{ state: 'loading' }} />)
    expect(screen.getByRole('region', { name: 'Unlinked sessions' })).toHaveTextContent('Unlinked sessions · reading')
    rerender(<UnlinkedSessions read={{ state: 'ok', data, stale: true }} />)
    expect(screen.getByRole('region', { name: 'Unlinked sessions' })).toHaveTextContent('Unlinked sessions · last read 3')
  })
  it('says an answer from a platform before P-c2 carries no binding, never that nothing is registered', () => {
    const before = { ...data, threads: data.threads.map(t => { const { binding: _b, server_note: _n, ...rest } = t; return rest as AgentThread }) }
    const out = unlinkedSessions(before)
    // Every row, the Codex one too: an answer that judges nothing marks nothing unsupported either.
    expect(out.rows).toHaveLength(7)
    for (const row of out.rows) {
      expect(row.state).toBe('unknown')
      expect(row.reason).toBe('No binding in this answer: the platform is a version that does not judge sessions')
    }
    render(<UnlinkedSessions read={ok(before)} />)
    expect(screen.queryByText(/No registration recorded/)).toBeNull()
    expect(screen.getAllByText(/No binding in this answer/)).toHaveLength(7)
    // A binding Unknown with no note says Unknown, not none recorded.
    expect(unlinkedSessions({ ...data, threads: [thread({ thread: 'q', binding: { state: 'unknown' } })] }).rows[0].reason).toBe('Binding Unknown')
  })
  it('keeps the Coverage line as it was for a platform before P-c2', () => {
    expect(coverageGroups(parsePlanSnapshot(planView, 'R4').coverage)).toEqual(['0 unregistered · Not reconciled'])
    expect(coverageGroups({ ...view().coverage, sessions: { linked: 0, unlinked: 0, refused: 0, unsupported: 2, unknown: 1, scope: 's' } }))
      .toEqual(['0 unregistered · Not reconciled', 'Sessions · 0 linked · 0 unlinked · 0 refused · binding rate N/A', 'Not judged · 2 unsupported · 1 unknown'])
  })
})

// P-c2 pre-review M5: Go writes fractional seconds without trailing zeros, so instants are compared by time, not text.
describe('session rows by time', () => {
  it('orders instants of different fraction widths by time', () => {
    expect(compareInstant('2026-11-04T12:00:00.9Z', '2026-11-04T12:00:00.91Z')).toBeLessThan(0)
    expect(compareInstant('2026-11-04T12:00:01Z', '2026-11-04T12:00:00.999999999Z')).toBeGreaterThan(0)
    expect(compareInstant('2026-11-04T12:00:00.5Z', '2026-11-04T12:00:00.500Z')).not.toBe(0)
    expect(compareInstant('2026-11-04T12:00:00.000000001Z', '2026-11-04T12:00:00Z')).toBeGreaterThan(0)
  })
  it('takes the later row of a session when the earlier one has the shorter fraction', () => {
    const base = (threads as unknown as AgentThreadsResponse).threads[0]
    const older = { ...base, thread: 's', session: 's', epoch: 1, host: 'h1', at: '2026-11-04T12:00:00.9Z', event: 'turn_end' } as AgentThread
    const newer = { ...base, thread: 's:1', session: 's', epoch: 1, host: 'h1', at: '2026-11-04T12:00:00.91Z', event: 'turn_start' } as AgentThread
    const [only] = latestBySession([older, newer])
    expect(only.thread).toBe('s:1')
    expect(latestBySession([newer, older])[0].thread).toBe('s:1')
  })
})
