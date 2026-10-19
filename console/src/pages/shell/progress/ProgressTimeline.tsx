import { useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { fetchEventsAfter, fetchLatestEvents, type RuntimeEventRow } from '@/api/agentRuntimeEvents'
import { fetchReleaseRecords } from '@/api/releases'
import { RELEASE_RECORD_LIMIT } from '@/pages/shell/releases/releaseView'
import { clockTime } from '../owner/ownerModel'
import { mergeEvents, taskTitles, timelineRows, TIMELINE_PAGE, TIMELINE_TYPES, type TimelineKind } from './timelineModel'

const TIMELINE_KEY = ['agent-runtime-timeline']
/** A read on stops after this many full pages; the next poll goes on from where it stopped. */
const READ_ON_PAGES = 5
const TONE: Partial<Record<TimelineKind, string>> = {
  alert: 'var(--color-danger)', failed: 'var(--color-danger)', rejected: 'var(--color-warning)', stop: 'var(--color-warning)',
  accepted: 'var(--color-success)', step: 'var(--color-success)', release: 'var(--color-info)',
}
type Read = { events: RuntimeEventRow[]; complete: boolean }
const message = (e: unknown) => e instanceof Error ? e.message : 'Request failed'

/**
 * The latest events first, then on from the cursor each read gives (`next`), each event kept once by
 * its id; back from the oldest event a latest or back read listed, never from one read on (an event
 * read on may have a lower id than events shown before it).
 */
function useTimeline() {
  const kept = useRef({ events: new Map<number, RuntimeEventRow>(), cursor: '', back: 0, complete: false })
  const client = useQueryClient()
  const [older, setOlder] = useState({ pending: false, error: '' })
  const snapshot = (): Read => ({ events: [...kept.current.events.values()], complete: kept.current.complete })
  const query = useQuery({ queryKey: TIMELINE_KEY, retry: false, gcTime: 0, refetchInterval: 15_000,
    queryFn: async ({ signal }) => {
      const s = kept.current
      if (!s.cursor) {
        const page = await fetchLatestEvents({ types: TIMELINE_TYPES, limit: TIMELINE_PAGE }, signal)
        mergeEvents(s.events, page.events)
        s.back = page.events.at(-1)?.event_id ?? 0
        s.complete = page.events.length < TIMELINE_PAGE
        s.cursor = page.next
      } else {
        for (let i = 0; i < READ_ON_PAGES; i++) {
          const page = await fetchEventsAfter(s.cursor, { types: TIMELINE_TYPES, limit: TIMELINE_PAGE }, signal)
          mergeEvents(s.events, page.events)
          s.cursor = page.next
          if (page.events.length < TIMELINE_PAGE) break
        }
      }
      return snapshot()
    } })
  async function readOlder() {
    const s = kept.current
    if (older.pending || s.complete || !s.back) return
    setOlder({ pending: true, error: '' })
    try {
      const page = await fetchLatestEvents({ types: TIMELINE_TYPES, limit: TIMELINE_PAGE, before: s.back })
      mergeEvents(s.events, page.events)
      s.back = page.events.at(-1)?.event_id ?? s.back
      s.complete = page.events.length < TIMELINE_PAGE
      client.setQueryData(TIMELINE_KEY, snapshot())
      setOlder({ pending: false, error: '' })
    } catch (e) { setOlder({ pending: false, error: message(e) }) }
  }
  return { query, older, readOlder }
}

/** History's timeline: from the runtime's events and the platform's release records, newest first. */
export function ProgressTimeline({ runtime, now }: { runtime: RuntimeProgress | undefined; now: number }) {
  const { query, older, readOlder } = useTimeline()
  const releasesQ = useQuery({ queryKey: ['releases', 'records', RELEASE_RECORD_LIMIT], queryFn: () => fetchReleaseRecords(RELEASE_RECORD_LIMIT), refetchInterval: 30_000 })
  const data = query.data
  const rows = data ? timelineRows(data.events, releasesQ.data?.records ?? [], { titles: taskTitles(runtime), complete: data.complete }) : []
  return <section aria-label="Timeline" className="flex min-w-0 flex-col gap-2">
    <h4 className="m-0">Timeline</h4>
    {query.isPending && <p role="status" className="m-0 text-sm">Reading the runtime's events…</p>}
    {query.isError && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">The runtime's events could not be read: {query.error.message}{data ? '. The events below were read earlier.' : ''}</p>}
    {releasesQ.isError && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Release records could not be read: {releasesQ.error.message}</p>}
    {data && rows.length === 0 && <p className="m-0 text-sm text-muted-foreground">{data.complete ? 'Nothing has happened yet.' : 'Nothing to show among the latest events; read older ones.'}</p>}
    <ol aria-label="Timeline" className="m-0 flex list-none flex-col gap-1.5 p-0">{rows.map(row => <li key={row.key} data-kind={row.kind} className="flex min-w-0 flex-col gap-0.5 text-sm">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        <time dateTime={row.at} title={row.at} className="shrink-0 font-mono text-xs text-muted-foreground">{clockTime(Date.parse(row.at), now)}</time>
        <span className="shrink-0 font-semibold" style={{ color: TONE[row.kind] }}>{row.label}</span>
        <span className="min-w-0 [overflow-wrap:anywhere]">{row.text}</span>
        {row.mission && <a href={`#progress?goal=${encodeURIComponent(row.mission)}`} className="shrink-0 text-xs">{row.mission}</a>}
      </div>
      {row.detail && <span className="min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">{row.detail}</span>}
    </li>)}</ol>
    {older.error && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Older events could not be read: {older.error}</p>}
    {data && !data.complete && <button type="button" disabled={older.pending} onClick={() => { void readOlder() }}
      className="min-h-11 self-start text-sm underline disabled:opacity-60">{older.pending ? 'Reading older events…' : 'Show older'}</button>}
  </section>
}
