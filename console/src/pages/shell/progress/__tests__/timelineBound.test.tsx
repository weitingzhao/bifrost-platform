import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import latest from '@/api/__fixtures__/agentrt-events-across.testdb.json'
import afterPage from '@/api/__fixtures__/agentrt-events-after-page.testdb.json'
import { trimEvents } from '../timelineModel'
import { parseEventPage, type RuntimeEventRow } from '@/api/agentRuntimeEvents'

// GET /agentrt/events as the handler answered it on a test database (agentrt-events-*.testdb.json):
// pages of five, and six shown events kept from reading on, so a page left open stays bounded (review 411 B2).
vi.mock('../timelineModel', async original => ({ ...await original<object>(), TIMELINE_PAGE: 5, TIMELINE_KEEP: 6 }))
const { ProgressTimeline } = await import('../ProgressTimeline')

function json(body: unknown) { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
const clients: QueryClient[] = []
beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { cleanup(); clients.forEach(c => c.clear()); clients.length = 0; vi.unstubAllGlobals() })

describe('a timeline left open (review 411 B2)', () => {
  it('keeps the events of the largest ids and says where the rest went', () => {
    const kept = new Map<number, RuntimeEventRow>(parseEventPage(latest).events.slice(0, 8).map(e => [e.event_id, e]))
    expect(trimEvents(kept, 8)).toBeNull()
    const cut = trimEvents(kept, 5)!
    expect([...kept.keys()]).toEqual([78, 77, 76, 75, 74])
    expect(cut.smallestKept).toBe(74)
    expect(cut.droppedUntil).toBe(Date.parse(latest.events[5].at))
  })

  it('keeps the newest shown events plus what Show older loaded, and goes back from where the window starts', async () => {
    const calls: Record<string, string>[] = []
    let tick = 0
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'http://x')
      if (url.pathname === '/api/v1/releases') return json({ status: {}, records: [] })
      const q = Object.fromEntries(url.searchParams)
      calls.push(q)
      if (q.before_event_id) return json({ events: latest.events.slice(5, 10), next: '2173743.0' })
      // Each read on brings events committed late with lower ids: the window keeps the largest.
      if (q.after) return json({ events: tick++ === 0 ? afterPage.events.slice(0, 4) : afterPage.events.slice(8, 12), next: `2173743.${tick}` })
      return json({ events: latest.events.slice(0, 5), next: '2173743.0' })
    }))
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); clients.push(client)
    render(<QueryClientProvider client={client}><ProgressTimeline runtime={undefined} now={Date.parse('2026-10-19T23:40:00Z')} /></QueryClientProvider>)
    await screen.findByRole('button', { name: 'Show older' })
    const list = screen.getByRole('list', { name: 'Timeline' })
    const ids = () => within(list).getAllByRole('listitem').length

    // Five shown, four more read on (imports 18–21): six kept, the rest behind Show older.
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-timeline'] }) })
    await waitFor(() => expect(ids()).toBe(6))
    expect(screen.getByText('Only the newest events are kept on this page; older ones are behind Show older.')).toBeInTheDocument()

    // Show older loads two shown events (70, 69): the window grows by them.
    fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
    await waitFor(() => expect(ids()).toBe(8))
    expect(calls.at(-1)).toMatchObject({ before_event_id: '74' })

    // Four more read on (27, 28, 30, 31): still eight kept.
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-timeline'] }) })
    await waitFor(() => expect(ids()).toBe(8))
    fireEvent.click(screen.getByRole('button', { name: 'Show older' }))
    await waitFor(() => expect(calls.at(-1)).toMatchObject({ before_event_id: '65' }))
  })
})
