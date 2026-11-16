/**
 * wait_for_event (the bus, step 1: api/internal/bus/RULES.md §8 item 4): one
 * long poll of GET /api/v1/bus/events, the platform's gate on the ledger.
 * index.ts registers the tool; the query string is built and tested here.
 */
export const BUS_WAIT_MAX_SECONDS = 25

export interface WaitForEventArgs {
  types: string
  match?: string
  after?: string
  wait_seconds?: number
  client?: string
}

export const busEventsPath = (a: WaitForEventArgs): string => {
  const q = new URLSearchParams()
  q.set('types', a.types)
  if (a.match !== undefined && a.match !== '') q.set('match', a.match)
  if (a.after !== undefined && a.after !== '') q.set('after', a.after)
  q.set('wait', String(Math.min(Math.max(0, a.wait_seconds ?? BUS_WAIT_MAX_SECONDS), BUS_WAIT_MAX_SECONDS)))
  q.set('client', a.client !== undefined && a.client !== '' ? a.client : 'mcp')
  return '/api/v1/bus/events?' + q.toString()
}

export const WAIT_FOR_EVENT_DESCRIPTION =
  'Waits for platform events (read-only; it changes nothing). One long poll of the platform\'s event gate, up to wait_seconds (max 25), for the next event of the given types (comma list of registered types, e.g. git.main.advanced) whose subject contains match. Returns {events: [CloudEvents envelope], next}. An event is a pointer, not the state: it carries no data; read the current state at its ref (a platform read route) before acting, since events can be late, repeated or lost. Keep next and pass it as after on the following call to resume without gaps; left out, the wait starts from now. Each event has a stable id (ev-<n>) to drop repeats, and its cursor. client labels you in the delivery receipts (at most 40 characters).'
