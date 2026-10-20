import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'

/** The most events GET /agentrt/events?stream= answers a page with: the handler's fixed LIMIT (agentrtapi/api.go getEvents). */
export const STREAM_PAGE = 500

type StreamEvent = { seq: number; event_stream_id?: unknown } & Record<string, unknown>

/**
 * One page of GET /agentrt/events?stream=<s>&after_seq=<n> as the handler answers it: the events with
 * seq above after_seq, ascending by seq, at most STREAM_PAGE of them, as a bare array.
 *
 * It models only that one path (review 418): events already of one stream and sorted by seq, and an
 * after_seq that is a non-negative safe integer. It does not sort, filter by stream or answer the
 * handler's 400s, so it refuses whatever it does not model rather than answer unlike the handler.
 */
export function streamPage(events: readonly StreamEvent[], url: string): StreamEvent[] {
  const params = new URL(url, 'http://console.test').searchParams
  const raw = params.get('after_seq') ?? '0'
  const after = Number(raw)
  if (!/^\d+$/.test(raw) || !Number.isSafeInteger(after)) throw new Error(`streamPage: after_seq ${raw} is not a non-negative safe integer`)
  const streams = new Set(events.map(e => e.event_stream_id))
  if (streams.size > 1) throw new Error(`streamPage: events of ${streams.size} streams; it models one`)
  const stream = params.get('stream')
  if (stream !== null && events.length > 0 && !streams.has(stream)) throw new Error(`streamPage: asked for ${stream}, given ${[...streams].join()}`)
  if (events.some((e, i) => !Number.isSafeInteger(e.seq) || (i > 0 && e.seq <= events[i - 1].seq))) throw new Error('streamPage: events not sorted by seq')
  return events.filter(e => e.seq > after).slice(0, STREAM_PAGE)
}

/**
 * The plan stream of the imported fixture with `before` other events ahead of its import head, as a
 * stream that held notes before the import does (TestPlanImportHeadAfterOtherEvents): the fixture's
 * rows, numbered on from 1. Without the head, the stream of a plan that was never imported.
 */
export function planStream(before: number, head = true): StreamEvent[] {
  const created = imported.events.find(e => e.event_type === 'plan_node_created')!
  const notes = Array.from({ length: before }, (_, i) => ({ ...created, event_id: i + 1, seq: i + 1, event_type: 'note',
    plan_node_id: null, body_json: { text: `before the import ${i}` } }))
  const rows = (head ? imported.events : imported.events.filter(e => e.event_type !== 'legacy_imported'))
    .map((e, i) => ({ ...e, event_id: before + i + 1, seq: before + i + 1 }))
  return [...notes, ...rows]
}
