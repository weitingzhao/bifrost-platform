import imported from '@/api/__fixtures__/agentrt-plan-imported.testdb.json'

/** The most events GET /agentrt/events?stream= answers a page with (agentrtapi/api.go getEvents). */
export const STREAM_PAGE = 500

type StreamEvent = { seq: number } & Record<string, unknown>

/**
 * One page of GET /agentrt/events?stream=<s>&after_seq=<n> as the handler answers it: a bare array of
 * the stream's events with seq above after_seq, ascending by seq, at most STREAM_PAGE of them.
 */
export function streamPage(events: readonly StreamEvent[], url: string): StreamEvent[] {
  const after = Number(new URL(url, 'http://console.test').searchParams.get('after_seq') ?? '0')
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
