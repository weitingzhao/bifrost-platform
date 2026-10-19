import { authedFetch } from '@/api/client'
import { record, text, type RuntimeRecord } from '@/api/agentRuntime'

/**
 * GET /api/v1/agentrt/events without a stream (2.5 G6): the runtime's events across every stream, each
 * row as `to_jsonb` of agentrt.event (agentrtapi/events.go). Two forms answer `{events, next}`:
 * the latest, newest first, back by `before_event_id`; and on from a cursor (`after=<txid>.<event_id>`),
 * oldest first. `next` is where to read on from; a reader keeps each event once by its id.
 */
export type RuntimeEventRow = {
  event_id: number; event_stream_id: string; seq: number; event_type: string; actor_id: string
  mission_id: string; plan_node_id: string; task_id: string; attempt_id: string; body: RuntimeRecord; at: string
}
export type RuntimeEventPage = { events: RuntimeEventRow[]; next: string }

const CURSOR = /^\d+\.\d+$/
const invalid = (): never => { throw new Error('Agent events: invalid platform response.') }
const whole = (v: unknown): v is number => typeof v === 'number' && Number.isInteger(v) && v > 0

/** Reject a malformed page rather than show a timeline with holes nobody can see. */
export function parseEventPage(value: unknown): RuntimeEventPage {
  const r = record(value)
  if (!Array.isArray(r.events) || typeof r.next !== 'string' || !CURSOR.test(r.next)) return invalid()
  const events = r.events.map(v => {
    const e = record(v)
    if (!whole(e.event_id) || !whole(e.seq) || typeof e.event_type !== 'string' || e.event_type === '' || typeof e.event_stream_id !== 'string'
      || typeof e.at !== 'string' || !Number.isFinite(Date.parse(e.at)) || e.body_json === null || typeof e.body_json !== 'object' || Array.isArray(e.body_json)) return invalid()
    return { event_id: e.event_id, event_stream_id: e.event_stream_id, seq: e.seq, event_type: e.event_type, actor_id: text(e.actor_id),
      mission_id: text(e.mission_id), plan_node_id: text(e.plan_node_id), task_id: text(e.task_id), attempt_id: text(e.attempt_id),
      body: record(e.body_json), at: e.at }
  })
  return { events, next: r.next }
}

async function read(params: URLSearchParams, signal?: AbortSignal): Promise<RuntimeEventPage> {
  const r = await authedFetch('Agent events', `/api/v1/agentrt/events?${params}`, { signal })
  return parseEventPage(await r.json())
}

/** The latest events of these types, newest first; `before` reads back from an event id. */
export function fetchLatestEvents(o: { types: readonly string[]; limit: number; before?: number }, signal?: AbortSignal) {
  const params = new URLSearchParams({ limit: String(o.limit), types: o.types.join(',') })
  if (o.before !== undefined) params.set('before_event_id', String(o.before))
  return read(params, signal)
}

/** The events of these types committed since the cursor, oldest first. */
export function fetchEventsAfter(cursor: string, o: { types: readonly string[]; limit: number }, signal?: AbortSignal) {
  return read(new URLSearchParams({ after: cursor, limit: String(o.limit), types: o.types.join(',') }), signal)
}
