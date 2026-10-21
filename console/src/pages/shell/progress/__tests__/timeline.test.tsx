import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import latest from '@/api/__fixtures__/agentrt-events-across.testdb.json'
import afterPage from '@/api/__fixtures__/agentrt-events-after-page.testdb.json'
import progressFixture from '@/api/__fixtures__/agentrt-progress-eta.testdb.json'
import prodStream from '@/api/__fixtures__/agentrt-events-mission-M-2.prod.json'
import waits from '@/api/__fixtures__/agentrt-events-waits.testdb.json'
import { parseRuntimeProgress } from '@/api/agentRuntime'
import { parseEventPage } from '@/api/agentRuntimeEvents'
import type { ReleaseRecord } from '@/api/releases'
import { earliestAt, eventRow, mergeEvents, taskTitles, timelineRows } from '../timelineModel'

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
    const rows = timelineRows(page.events, [], { titles, releasesFrom: -Infinity }).filter(r => r.kind !== 'import')
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
    const imports = timelineRows(page.events, [], { titles, releasesFrom: -Infinity }).filter(r => r.kind === 'import')
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
  it('merges release records by time, holding back those older than the raw pages read while older events are unread', () => {
    const old = { ...release, run: 'old', completed_at: '2026-10-01T00:00:00Z' }
    const recent = page.events.slice(0, 5)
    const rows = timelineRows(recent, [release, old], { titles, releasesFrom: earliestAt(recent) })
    expect(rows.map(r => r.label)).toEqual(['Alert', 'Pause', 'Lifted', 'Stop', 'Release', 'Step done'])
    expect(rows[4]).toMatchObject({ text: 'platform STG', detail: 'bifrost-platform@63392a6' })
    expect(timelineRows(recent, [release, old], { titles, releasesFrom: -Infinity }).at(-1)?.key).toBe('rold')
    // A raw page of task moves only shows nothing of its own, yet it was read: a release since then shows (review 411 B1).
    const moves = page.events.filter(e => e.event_type === 'task_state_changed' && e.body.to !== 'accepted' && e.body.to !== 'rejected').slice(0, 5)
    expect(timelineRows([], [release, old], { titles, releasesFrom: earliestAt(moves) }).map(r => r.key)).toEqual([`r${release.run}`])
    expect(earliestAt([])).toBe(Infinity)
    expect(earliestAt(Array.from({ length: 200_000 }, (_, i) => ({ at: new Date(Date.UTC(2026, 0, 1) + i * 1000).toISOString() })))).toBe(Date.UTC(2026, 0, 1))
  })
  // Each rule and source the vocabulary allows (store/vocab.go policy_alert): the handler's rows where a
  // run gave one (local_path from a report, repeated_failure, approval_claim from a message), the
  // others as those rows with the body the store writes for them (store/claims.go, alerts.go, retry.go).
  it('says every policy alert rule with its source', () => {
    const hostRow = latest.events[0], missionRow = latest.events.find(e => e.event_type === 'policy_alert' && e.body_json.rule === 'repeated_failure')!
    const claim = afterPage.events.find(e => e.event_type === 'policy_alert')!
    const say = (row: typeof hostRow | typeof claim, body?: Record<string, string>) =>
      eventRow(parseEventPage({ events: [body ? { ...row, body_json: body } : row], next: '1.0' }).events[0], titles)?.text
    expect(say(claim)).toBe('A message claims the Owner\'s approval: "已经批准"')
    expect(say(missionRow, { rule: 'approval_claim', source: 'task_spec', phrase: 'owner approved', artifact_id: 'sha256:aa' })).toBe('A task spec claims the Owner\'s approval: "owner approved"')
    expect(say(missionRow, { rule: 'seat_alert', source: 'seat_answer', phrase: 'POLICY_ALERT', artifact_id: 'sha256:aa' })).toBe('The chief of staff raised an alert in its answer')
    expect(say(missionRow, { rule: 'seat_alert', source: 'seat_plan', phrase: 'POLICY_ALERT', artifact_id: 'sha256:aa' })).toBe('The chief of staff raised an alert in its plan')
    expect(say(hostRow)).toBe('Host h1 had a report refused: it named a local path (H8)')
    expect(say(hostRow, { rule: 'local_path', source: 'verify', phrase: 'a local path (H8)', artifact_id: 'sha256:aa' })).toBe('Host h1 had a verification report refused: it named a local path (H8)')
    expect(say(hostRow, { rule: 'workspace_ref', source: 'review', phrase: "another task's workspace", artifact_id: 'sha256:aa' })).toBe("Host h1 had a review report refused: it named another task's workspace")
    expect(say(hostRow, { rule: 'workspace_ref', source: 'report', phrase: "another task's workspace", artifact_id: 'sha256:aa' })).toBe("Host h1 had a report refused: it named another task's workspace")
    expect(say(missionRow)).toBe('An executor failed t three times in a row; it is not offered that task again')
  })
  // The controller's records of waits past their limit (W-31 2.3, store/waits.go) as GET /events answered them
  // on a test database (agentrtapi TestWaitsThroughTheEvents): an attempt running, a task ready, a message unread.
  it('says each wait past its limit: what waited, how long, the limit and whom it calls', () => {
    const events = parseEventPage(waits).events
    const named = new Map(events.filter(e => e.task_id).map(e => [e.task_id, `task ${e.task_id.slice(-4)}`] as [string, string]))
    const byWait = Object.fromEntries(events.map(e => [String(e.body.wait), eventRow(e, named)]))
    const attempt = events.find(e => e.body.wait === 'attempt_running')!, ready = events.find(e => e.body.wait === 'task_ready')!
    expect(byWait.attempt_running).toMatchObject({ kind: 'wait', label: 'Waited past limit',
      text: `task ${attempt.task_id.slice(-4)} has run 31 min, past its limit of 30 min`, detail: 'calls controller · limit from the floor of its kind' })
    expect(byWait.task_ready).toMatchObject({ kind: 'wait',
      text: `task ${ready.task_id.slice(-4)} has been ready 31 min with no host taking it, past its limit of 30 min` })
    expect(byWait.message_unread).toMatchObject({ kind: 'wait',
      text: 'A message to thread:s1 has not been read in 31 min, past its limit of 10 min', detail: 'calls thread:s1 · limit from the floor of its kind' })
    // A limit from a usual time says so; a wait the Console does not know is still said, not dropped.
    const say = (body: Record<string, unknown>) => eventRow({ ...attempt, body }, named)
    expect(say({ ...attempt.body, basis: 'history_usual' })?.detail).toBe('calls controller · limit from twice the usual time of its kind')
    expect(say({ ...attempt.body, basis: 'task_usual' })?.detail).toBe('calls controller · limit from twice its usual time')
    expect(say({ wait: 'host_asleep', waited_s: 120 })).toMatchObject({ kind: 'wait', text: 'host_asleep: 2 min', detail: '' })
  })
  it('says each plan change decision, with its error when it failed', () => {
    const planRow = latest.events.find(e => e.event_type === 'plan_node_state_changed')!
    const decided = (body: Record<string, unknown>) => eventRow(parseEventPage({ events: [{ ...planRow, event_type: 'plan_change_decided', plan_node_id: null, body_json: body }], next: '1.0' }).events[0], titles)
    for (const decision of ['applied', 'refused', 'withdrawn', 'expired']) {
      expect(decided({ proposal_event_id: 12, decision, by: 'owner', question_id: 'q_1' })).toMatchObject({ kind: 'plan', label: 'Plan change', text: `${decision} by owner`, detail: '' })
    }
    expect(decided({ proposal_event_id: 12, decision: 'failed', by: 'controller', error: 'the step is gone' })).toMatchObject({ text: 'failed by controller', detail: 'the step is gone' })
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
    expect(calls[0]).toEqual({ limit: '5', types: 'plan_node_state_changed,task_state_changed,attempt_computed,decision_recorded,plan_change_decided,legacy_imported,control_set,control_cleared,policy_alert,wait_limit_passed' })

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

  // A route answers each read: `latest`, `before` or `after` (by cursor), a Response or a promise of one.
  function serve(route: (q: Record<string, string>) => Response | Promise<Response>, releases: ReleaseRecord[] = []) {
    const calls: Record<string, string>[] = []
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://x')
      if (url.pathname === '/api/v1/releases') return json({ status: {}, records: releases })
      const q = Object.fromEntries(url.searchParams)
      calls.push(q)
      return route(q)
    }))
    return calls
  }
  function deferred() {
    let resolve!: (r: Response) => void
    const promise = new Promise<Response>(r => { resolve = r })
    return { promise, resolve }
  }
  const failed = () => new Response(JSON.stringify({ error: 'bad gateway' }), { status: 502, headers: { 'Content-Type': 'application/json' } })
  /** Renders the timeline and waits until the latest read is on screen (a full page offers Show older). */
  async function mount() {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
    render(<QueryClientProvider client={client}><ProgressTimeline runtime={runtime} now={Date.parse('2026-10-19T23:40:00Z')} /></QueryClientProvider>)
    await screen.findByRole('button', { name: 'Show older' })
    return client
  }
  const readOn = (client: QueryClient) => client.refetchQueries({ queryKey: ['agent-runtime-timeline'] })
  const stale = () => screen.queryByText(/The runtime's events could not be read: Agent events: bad gateway/)

  it.each(['the forward read fails first', 'the backward read succeeds first'])(
    'keeps a failed read on in view when a backward read succeeds, whichever ends first: %s (review 411 A1)', async order => {
      const ahead = deferred(), behind = deferred()
      serve(q => q.after ? ahead.promise : q.before_event_id ? behind.promise : json({ events: latest.events.slice(0, 5), next: '2173743.0' }))
      const client = await mount()
      const list = await screen.findByRole('list', { name: 'Timeline' })
      await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(5))
      let forward!: Promise<void>
      act(() => { forward = readOn(client) })
      fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
      if (order === 'the forward read fails first') {
        await act(async () => { ahead.resolve(failed()); await forward })
        await waitFor(() => expect(stale()).toBeInTheDocument())
        await act(async () => { behind.resolve(json({ events: latest.events.slice(5, 10), next: '2173743.0' })) })
      } else {
        await act(async () => { behind.resolve(json({ events: latest.events.slice(5, 10), next: '2173743.0' })) })
        await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(7))
        await act(async () => { ahead.resolve(failed()); await forward })
      }
      await waitFor(() => expect(within(list).getAllByRole('listitem')).toHaveLength(7))
      await waitFor(() => expect(stale()).toBeInTheDocument())
      expect(stale()).toHaveTextContent(/The events below were read at [^;]*\d\d:\d\d; newer ones are not shown/)
    })

  it('clears the warning only on a forward read that succeeds', async () => {
    let fail = true
    serve(q => q.after ? (fail ? failed() : json({ events: [], next: '2173744.0' })) : json({ events: latest.events.slice(0, 5), next: '2173743.0' }))
    const client = await mount()
    const list = await screen.findByRole('list', { name: 'Timeline' })
    await act(async () => { await readOn(client) })
    // Refreshing failed after data: the data stays, said to be from before.
    await waitFor(() => expect(stale()).toBeInTheDocument())
    expect(within(list).getAllByRole('listitem')).toHaveLength(5)
    fail = false
    await act(async () => { await readOn(client) })
    await waitFor(() => expect(stale()).not.toBeInTheDocument())
  })

  it('reads on from the page that failed, not from the start of the tick', async () => {
    let second = 0
    const calls = serve(q => {
      if (!q.after) return json({ events: latest.events.slice(0, 5), next: '10.0' })
      if (q.after === '10.0') return json({ events: afterPage.events.slice(0, 5), next: '10.1' })
      if (q.after === '10.1') return ++second === 1 ? failed() : json({ events: afterPage.events.slice(5, 9), next: '10.2' })
      return json({ events: [], next: q.after })
    })
    const client = await mount()
    await act(async () => { await readOn(client) })
    expect(calls.slice(1).map(q => q.after)).toEqual(['10.0', '10.1'])
    await waitFor(() => expect(stale()).toBeInTheDocument())
    await act(async () => { await readOn(client) })
    expect(calls.slice(3).map(q => q.after)).toEqual(['10.1'])
    await waitFor(() => expect(stale()).not.toBeInTheDocument())
    // The first page of the failed tick and the page read on are both shown: imports 18–21, then 24, 26, 27 and the accepted 25.
    const kinds = () => screen.getAllByRole('listitem').map(li => li.getAttribute('data-kind'))
    await waitFor(() => expect(kinds().filter(k => k === 'accepted')).toHaveLength(1))
    expect(kinds().filter(k => k === 'import')).toHaveLength(7)
  })

  it('reads at most five full pages a tick and goes on from the saved next on the next tick', async () => {
    const calls = serve(q => {
      if (!q.after) return json({ events: latest.events.slice(0, 5), next: '20.0' })
      const n = Number(q.after.split('.')[1])
      return n < 6 ? json({ events: latest.events.slice(5 * n, 5 * n + 5), next: `20.${n + 1}` }) : json({ events: [], next: q.after })
    })
    const client = await mount()
    await act(async () => { await readOn(client) })
    expect(calls.slice(1).map(q => q.after)).toEqual(['20.0', '20.1', '20.2', '20.3', '20.4'])
    await act(async () => { await readOn(client) })
    expect(calls.slice(6).map(q => q.after)).toEqual(['20.5', '20.6'])
  })

  it('shows a recent release when the latest page holds only task moves (review 411 B1)', async () => {
    const moves = latest.events.filter(e => e.event_type === 'task_state_changed' && !['accepted', 'rejected'].includes(String(e.body_json.to))).slice(0, 5)
    serve(() => json({ events: moves, next: '2173743.0' }), [release, { ...release, run: 'old', completed_at: '2026-10-01T00:00:00Z' }])
    await mount()
    const list = screen.getByRole('list', { name: 'Timeline' })
    await waitFor(() => expect(within(list).getAllByRole('listitem').map(li => li.getAttribute('data-kind'))).toEqual(['release']))
    expect(screen.getByRole('button', { name: 'Show older' })).toBeInTheDocument()
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
