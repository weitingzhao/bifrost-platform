import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import latest from '@/api/__fixtures__/agentrt-events-across.testdb.json'
import progressFixture from '@/api/__fixtures__/agentrt-progress-eta.testdb.json'
import prodStream from '@/api/__fixtures__/agentrt-events-mission-M-2.prod.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parseEventPage } from '@/api/agentRuntimeEvents'
import type { ReleaseRecord } from '@/api/releases'
import { eventRow, mergeEvents, taskTitles, timelineRows } from '../timelineModel'

// The events are GET /agentrt/events without a stream as the handler answered it on a test database
// (store's kernel world: three accepted implementations, one rejected after a failed attempt, an executor
// failing a task three times, a step done, a stop set and lifted, a pause, a report refused on host h1),
// with GET /agentrt/progress from the same database for the task titles. The PROD rows are mission M-2's.
vi.mock('../timelineModel', async original => ({ ...await original<object>(), TIMELINE_PAGE: 5 }))
const { ProgressTimeline } = await import('../ProgressTimeline')

const page = parseEventPage(latest)
const runtime = parseRuntimeProgress(progressFixture)
const titles = taskTitles(runtime)
const release: ReleaseRecord = { run: 'bifrost-deliver-platform-run-1', pipeline: 'bifrost-deliver-platform', lane: 'platform', env: 'stg', deploys: true,
  started_at: '2026-10-19T23:30:00Z', completed_at: '2026-10-19T23:35:51.330Z', repos: { 'bifrost-platform': { sha: '63392a67aa0e1f00', source: 'result' } },
  recorded_at: '2026-10-19T23:35:52Z' }

describe('the timeline from the runtime\'s events (2.5 G6)', () => {
  it('keeps steps done, tasks accepted or rejected, failed attempts, stops and alerts; leaves out the task moves between', () => {
    const rows = timelineRows(page.events, [], { titles, complete: true }).filter(r => r.kind !== 'import')
    expect(rows.map(r => [r.label, r.text])).toEqual([
      ['Alert', 'Host h1 had a report refused: it named a local path (H8)'],
      ['Pause', 'mission M-2 by owner'],
      ['Lifted', `${latest.events[2].body_json.control_id} by owner`],
      ['Stop', 'global by owner'],
      ['Step done', 'R2:2.1-2'],
      ['Alert', 'An executor failed t three times in a row; it is not offered that task again'],
      ['Rejected', 'ended'],
      ['Attempt failed', 'ended'],
      ['Accepted', 'ended'], ['Accepted', 'ended'], ['Accepted', 'ended'],
    ])
    expect(rows.find(r => r.label === 'Step done')).toMatchObject({ detail: 'test: the step is done' })
    expect(rows.find(r => r.label === 'Attempt failed')).toMatchObject({ detail: 'exit', mission: 'M-1' })
    expect(rows.find(r => r.text.startsWith('An executor'))?.detail).toMatch(/^attempt a_/)
    const imports = timelineRows(page.events, [], { titles, complete: true }).filter(r => r.kind === 'import')
    expect(imports.map(r => r.text)).toContain('test: an ended task')
  })
  it('reads PROD rows the same way: decisions with why, and a task without a title by its id', () => {
    const rows = parseEventPage({ events: prodStream, next: '1.0' }).events.map(e => eventRow(e, new Map())).filter(r => r !== null)
    const decisions = rows.filter(r => r.kind === 'decision')
    expect(decisions).toHaveLength(3)
    expect(decisions[0].text).toBe(prodStream.find(e => e.event_type === 'decision_recorded')!.body_json.decision)
    expect(decisions[0].detail).toContain('CLAUDE.md says code identifiers and UI strings are English')
    expect(rows.find(r => r.kind === 'accepted')?.text).toMatch(/^t_/)
  })
  it('keeps each event it shows once by id, and lets the task moves between go', () => {
    const kept = new Map<number, (typeof page.events)[number]>()
    mergeEvents(kept, page.events)
    mergeEvents(kept, page.events.slice(0, 3))
    expect(kept.size).toBe(page.events.filter(e => eventRow(e, titles) !== null).length)
    expect([...kept.values()].some(e => e.event_type === 'task_state_changed' && e.body.to === 'running')).toBe(false)
    expect(kept.size).toBeLessThan(page.events.length)
  })
  it('merges release records by time, holding back those older than the oldest event while older events are unread', () => {
    const old = { ...release, run: 'old', completed_at: '2026-10-01T00:00:00Z' }
    const recent = page.events.slice(0, 5)
    const rows = timelineRows(recent, [release, old], { titles, complete: false })
    expect(rows.map(r => r.label)).toEqual(['Alert', 'Pause', 'Lifted', 'Stop', 'Release', 'Step done'])
    expect(rows[4]).toMatchObject({ text: 'platform STG', detail: 'bifrost-platform@63392a6' })
    expect(timelineRows(recent, [release, old], { titles, complete: true }).at(-1)?.key).toBe('rold')
  })
})

function json(body: unknown) { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
const clients: QueryClient[] = []
beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals() })

describe('ProgressTimeline', () => {
  it('reads the latest, back from the oldest it listed, and on from next, keeping each event once', async () => {
    const decision = prodStream.find(e => e.event_type === 'decision_recorded')!
    const calls: Record<string, string>[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://x')
      if (url.pathname === '/api/v1/releases') return json({ status: {}, records: [release] })
      const q = Object.fromEntries(url.searchParams)
      calls.push(q)
      // Read on: one event listed again, and one that took a lower id and committed late.
      if (q.after) return json({ events: [latest.events[0], { ...decision, event_id: 50 }], next: '2173750.50' })
      if (q.before_event_id) return json({ events: latest.events.slice(5, 10), next: '2173743.0' })
      return json({ events: latest.events.slice(0, 5), next: '2173743.0' })
    }))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
    render(<QueryClientProvider client={client}><ProgressTimeline runtime={runtime} now={Date.parse('2026-10-19T23:40:00Z')} /></QueryClientProvider>)
    const list = await screen.findByRole('list', { name: 'Timeline' })
    await waitFor(() => expect(within(list).getAllByRole('listitem').map(li => li.getAttribute('data-kind'))).toEqual(['alert', 'stop', 'lifted', 'stop', 'release', 'step']))
    expect(calls[0]).toEqual({ limit: '5', types: 'plan_node_state_changed,task_state_changed,attempt_computed,decision_recorded,plan_change_decided,legacy_imported,control_set,control_cleared,policy_alert' })

    fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(8))
    expect(calls[1]).toMatchObject({ before_event_id: '74', limit: '5' })
    expect(within(list).getByText(/An executor failed t three times/)).toBeInTheDocument()

    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-timeline'] }) })
    expect(calls[2]).toMatchObject({ after: '2173743.0', limit: '5' })
    await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(9))
    expect(within(list).getAllByRole('listitem').filter(li => li.getAttribute('data-kind') === 'decision')).toHaveLength(1)
    expect(within(list).getAllByText('Host h1 had a report refused: it named a local path (H8)')).toHaveLength(1)
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-timeline'] }) })
    expect(calls[3]).toMatchObject({ after: '2173750.50' })
    // Back again from the oldest event a latest or back read listed, not from one read on.
    fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
    await waitFor(() => expect(calls[4]).toMatchObject({ before_event_id: '65' }))
  })

  it('says when the events cannot be read, and keeps the releases apart', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => String(input).includes('/releases')
      ? new Response('{}', { status: 502 }) : json({ events: latest.events, next: 'nope' })))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
    render(<QueryClientProvider client={client}><ProgressTimeline runtime={undefined} now={Date.now()} /></QueryClientProvider>)
    expect(await screen.findByText(/The runtime's events could not be read: Agent events: invalid platform response./)).toBeInTheDocument()
    expect(await screen.findByText(/Release records could not be read: releases: HTTP 502/)).toBeInTheDocument()
  })
})
