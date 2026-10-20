import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import latest from '@/api/__fixtures__/agentrt-events-across.testdb.json'
import type { ReleaseRecord } from '@/api/releases'
import { releasesFrom, trimEvents } from '../timelineModel'
import { parseEventPage, type RuntimeEventRow } from '@/api/agentRuntimeEvents'

// Pages of five and a window of six (review 411 B2, 413 B1, B2, C1). The server below keeps one list of
// events and answers the three forms as agentrtapi/events.go does: latest newest first, before_event_id
// below the id newest first, after on from a cursor oldest first, each at most `limit`. Its rows are the
// handler's own (agentrt-events-across.testdb.json: an accepted task's row), each given its own id and time.
vi.mock('../timelineModel', async original => ({ ...await original<object>(), TIMELINE_PAGE: 5, TIMELINE_KEEP: 6 }))
const { ProgressTimeline } = await import('../ProgressTimeline')

const accepted = latest.events.find(e => e.event_type === 'task_state_changed' && e.body_json.to === 'accepted')!
const BASE = Date.parse('2026-10-19T12:00:00Z')
type Row = typeof accepted
/** Event `id` at `minute` past noon: in `range` they are the same; ids and times need not run together. */
const event = (id: number, minute = id): Row => ({ ...accepted, event_id: id, seq: id, at: new Date(BASE + minute * 60_000).toISOString() })
const range = (from: number, to: number) => Array.from({ length: to - from + 1 }, (_, i) => event(from + i))
const release = (run: string, minute: number): ReleaseRecord => ({ run, pipeline: 'bifrost-deliver-platform', lane: 'platform', env: 'stg', deploys: true,
  started_at: new Date(BASE + (minute - 5) * 60_000).toISOString(), completed_at: new Date(BASE + minute * 60_000).toISOString(), repos: {}, recorded_at: '' })

function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }) }
const clients: QueryClient[] = []
beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals() })

/** A server over `rows`; a read on from a cursor in `failAfter` answers 502. */
function server(rows: Row[], releases: ReleaseRecord[] = []) {
  const state = { rows, failAfter: new Set<string>(), calls: [] as Record<string, string>[] }
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://x')
    if (url.pathname === '/api/v1/releases') return json({ status: {}, records: releases })
    const q = Object.fromEntries(url.searchParams)
    state.calls.push(q)
    const limit = Number(q.limit), top = state.rows.reduce((n, r) => Math.max(n, r.event_id), 0)
    const desc = [...state.rows].sort((a, b) => b.event_id - a.event_id)
    if (q.after) {
      if (state.failAfter.has(q.after)) return json({ error: 'bad gateway' }, 502)
      const from = Number(q.after.split('.')[1])
      const page = desc.filter(r => r.event_id > from).reverse().slice(0, limit)
      return json({ events: page, next: `1.${page.at(-1)?.event_id ?? from}` })
    }
    const before = q.before_event_id ? Number(q.before_event_id) : Infinity
    return json({ events: desc.filter(r => r.event_id < before).slice(0, limit), next: `1.${top}` })
  }))
  return state
}
async function mount() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
  render(<QueryClientProvider client={client}><ProgressTimeline runtime={undefined} now={BASE + 3600_000} /></QueryClientProvider>)
  return client
}
const readOn = (client: QueryClient) => act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-timeline'] }) })
const list = () => screen.getByRole('list', { name: 'Timeline' })
const kinds = () => within(list()).queryAllByRole('listitem').map(li => li.getAttribute('data-kind'))
/** The minutes past noon of the events on screen, newest first: their ids, for events made by `range`. */
const shown = () => within(list()).queryAllByRole('listitem').filter(li => li.getAttribute('data-kind') !== 'release')
  .map(li => (Date.parse(li.querySelector('time')!.getAttribute('dateTime')!) - BASE) / 60_000)
const older = () => fireEvent.click(screen.getByRole('button', { name: 'Show older' }))

describe('the window kept from reading on (model)', () => {
  it('keeps the largest ids and gives the smallest kept; every id let go is below it', () => {
    const kept = new Map<number, RuntimeEventRow>(parseEventPage({ events: range(1, 8), next: '1.8' }).events.map(e => [e.event_id, e]))
    expect(trimEvents(kept, 8)).toBeNull()
    expect(trimEvents(kept, 5)).toBe(4)
    expect([...kept.keys()].sort((a, b) => a - b)).toEqual([4, 5, 6, 7, 8])
  })
  it('shows releases from the earlier of the raw pages and the events shown, never later than an event shown', () => {
    const events = [event(7, 1), event(8, 2)]
    expect(releasesFrom(Infinity, events, false)).toBe(BASE + 60_000)
    expect(releasesFrom(BASE + 30 * 60_000, events, false)).toBe(BASE + 60_000)
    expect(releasesFrom(BASE, events, false)).toBe(BASE)
    expect(releasesFrom(BASE, events, true)).toBe(-Infinity)
  })
})

describe('a timeline left open (review 411 B2, 413 B2, C1)', () => {
  it('cuts the window as newer events come, keeps what Show older loaded, and reads back every event exactly once', async () => {
    const srv = server(range(1, 12))
    const client = await mount()
    await waitFor(() => expect(shown()).toEqual([12, 11, 10, 9, 8]))
    // Eight newer events in two pages: the window keeps six, and its lower edge moves up to 15.
    srv.rows = range(1, 20)
    await readOn(client)
    await waitFor(() => expect(shown()).toEqual([20, 19, 18, 17, 16, 15]))
    expect(screen.getByText('Only the newest events are kept on this page; older ones are behind Show older.')).toBeInTheDocument()
    older()
    await waitFor(() => expect(shown()).toEqual([20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10]))
    expect(srv.calls.at(-1)).toMatchObject({ before_event_id: '15' })
    // Six more: the window is 21–26 and Show older goes back from 21; what it loaded (10–14) stays.
    srv.rows = range(1, 26)
    await readOn(client)
    await waitFor(() => expect(shown()).toEqual([26, 25, 24, 23, 22, 21, 14, 13, 12, 11, 10]))
    for (const before of ['21', '16', '11', '6']) {
      older()
      await waitFor(() => expect(srv.calls.at(-1)).toMatchObject({ before_event_id: before }))
      await waitFor(() => expect(screen.getByRole('button', { name: 'Show older' })).toBeEnabled())
    }
    // Events 1–5 are a full page; the next read back is empty and the timeline is whole.
    older()
    await waitFor(() => expect(srv.calls.at(-1)).toMatchObject({ before_event_id: '1' }))
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Show older' })).not.toBeInTheDocument())
    expect(shown()).toEqual(Array.from({ length: 26 }, (_, i) => 26 - i))
    expect(new Set(shown()).size).toBe(26)
  })

  it('cuts after every page, so a page that fails next does not carry the window past it (review 413 B2-a)', async () => {
    const srv = server(range(1, 10))
    const client = await mount()
    await waitFor(() => expect(shown()).toEqual([10, 9, 8, 7, 6]))
    // Three ticks: a full page read on, then the next page fails.
    for (const top of [15, 20, 25]) {
      srv.rows = range(1, top + 5)
      srv.failAfter = new Set([`1.${top}`])
      await readOn(client)
    }
    expect(srv.calls.filter(q => q.after).map(q => q.after)).toEqual(['1.10', '1.15', '1.15', '1.20', '1.20', '1.25'])
    // A Show older that succeeds publishes what is kept: the window of six and the five it loaded.
    older()
    await waitFor(() => expect(shown()).toEqual([25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15]))
    expect(srv.calls.at(-1)).toMatchObject({ before_event_id: '20' })
  })

  it('shows a recent release after a first empty page and a cut (review 413 B1-a)', async () => {
    const srv = server([], [release('recent', 10), release('old', -600)])
    const client = await mount()
    await waitFor(() => expect(kinds()).toEqual(['release', 'release']))
    srv.rows = range(1, 7)
    await readOn(client)
    await waitFor(() => expect(shown()).toEqual([7, 6, 5, 4, 3, 2]))
    expect(kinds().filter(k => k === 'release')).toHaveLength(1)
    expect(kinds()[0]).toBe('release')
  })

  it('shows a release newer than an event still shown, though an event let go was later (review 413 B1-b)', async () => {
    // Ids and times apart: event 6 is at 12:10, events 7–10 at 12:01–12:04; the release at 12:05.
    const srv = server([event(6, 10), event(7, 1), event(8, 2), event(9, 3), event(10, 4)], [release('between', 5)])
    const client = await mount()
    await waitFor(() => expect(kinds()).toHaveLength(6))
    srv.rows = [...srv.rows, event(11, 20), event(12, 21)]
    await readOn(client)
    // The window keeps 7–12 and lets 6 go: the release at 12:05 is newer than 7 at 12:01, which is still shown.
    await waitFor(() => expect(shown()).toEqual([21, 20, 4, 3, 2, 1]))
    expect(kinds()).toEqual(['accepted', 'accepted', 'release', 'accepted', 'accepted', 'accepted', 'accepted'])
  })
})
