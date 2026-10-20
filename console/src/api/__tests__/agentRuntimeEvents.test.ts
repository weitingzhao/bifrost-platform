import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import latest from '../__fixtures__/agentrt-events-across.testdb.json'
import readOn from '../__fixtures__/agentrt-events-after.testdb.json'
import readOnPage from '../__fixtures__/agentrt-events-after-page.testdb.json'
import prodStream from '../__fixtures__/agentrt-events-mission-M-2.prod.json'
import { fetchEventsAfter, fetchLatestEvents, parseEventPage } from '../agentRuntimeEvents'

// The fixtures are GET /agentrt/events without a stream as the handler answered it on a test database
// (agentrtapi/events.go over the store's kernel world, 2026-10-19): the latest form, newest first, and
// a read on from 0.0 that the horizon held back. The PROD rows are the stream form's, the same to_jsonb rows.

function json(body: unknown) { return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } }) }
beforeEach(() => {
  // jsdom here has no working localStorage; the sign-in token is read from it on every request.
  Object.defineProperty(window, 'localStorage', { configurable: true, value: { getItem: () => null, setItem: () => undefined, removeItem: () => undefined, clear: () => undefined } })
})
afterEach(() => { vi.unstubAllGlobals() })

describe('events across streams (2.5 G6)', () => {
  it('reads the latest form: every row, newest first, with the cursor to read on from', () => {
    const page = parseEventPage(latest)
    expect(page.next).toBe('2173743.0')
    expect(page.events).toHaveLength(latest.events.length)
    expect(page.events.map(e => e.event_id)).toEqual([...page.events.map(e => e.event_id)].sort((a, b) => b - a))
    expect(page.events[0]).toEqual({ event_id: 78, event_stream_id: 'host:h1', seq: 2, event_type: 'policy_alert', actor_id: 'controller',
      mission_id: '', plan_node_id: '', task_id: '', attempt_id: '', at: latest.events[0].at, body: latest.events[0].body_json })
    const failed = page.events.find(e => e.event_type === 'attempt_computed' && e.body.conclusion === 'failed')!
    expect(failed).toMatchObject({ mission_id: 'M-1', task_id: expect.stringMatching(/^t_/), attempt_id: expect.stringMatching(/^a_/) })
    expect(parseEventPage(readOn)).toEqual({ events: [], next: '2173743.0' })
    // A read on that listed events (review 411 C1): oldest first by transaction, next at the horizon.
    const on = parseEventPage(readOnPage)
    expect(on.next).toBe('2230006.0')
    expect(on.events.map(e => e.event_id)).toEqual(readOnPage.events.map(e => e.event_id))
    expect(readOnPage.events.map(e => [e.txid, e.event_id])).toEqual([...readOnPage.events.map(e => [e.txid, e.event_id])].sort((a, b) => a[0] - b[0] || a[1] - b[1]))
    expect(on.events.at(-1)).toMatchObject({ event_type: 'policy_alert', event_stream_id: 'mbox:seat:chief_of_staff', body: { rule: 'approval_claim', source: 'message' } })
    expect(parseEventPage({ events: prodStream, next: '61980682.65' }).events).toHaveLength(prodStream.length)
  })
  it.each([
    ['the stream form', latest.events],
    ['no cursor', { events: [] }],
    ['an event id as the cursor', { events: [], next: '78' }],
    ['a row without its time', { events: [{ ...latest.events[0], at: undefined }], next: '1.2' }],
    ['a row with a body that is not an object', { events: [{ ...latest.events[0], body_json: [] }], next: '1.2' }],
    ['a row without an id', { events: [{ ...latest.events[0], event_id: '78' }], next: '1.2' }],
  ])('refuses %s rather than show a timeline with holes', (_, value) => {
    expect(() => parseEventPage(value)).toThrow('Agent events: invalid platform response.')
  })
  it('asks for the latest, back from an event, and on from a cursor, with the types', async () => {
    const fetch = vi.fn(async () => json(readOn))
    vi.stubGlobal('fetch', fetch)
    await fetchLatestEvents({ types: ['task_state_changed', 'policy_alert'], limit: 200 })
    await fetchLatestEvents({ types: ['policy_alert'], limit: 200, before: 42 })
    await fetchEventsAfter('2173743.0', { types: ['policy_alert'], limit: 200 })
    const urls = fetch.mock.calls.map(c => new URL(String((c as unknown[])[0]), 'http://x'))
    expect(urls.map(u => u.pathname)).toEqual(Array(3).fill('/api/v1/agentrt/events'))
    expect(urls.map(u => Object.fromEntries(u.searchParams))).toEqual([
      { limit: '200', types: 'task_state_changed,policy_alert' },
      { limit: '200', types: 'policy_alert', before_event_id: '42' },
      { after: '2173743.0', limit: '200', types: 'policy_alert' },
    ])
  })
})
