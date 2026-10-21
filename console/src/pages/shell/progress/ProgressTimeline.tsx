import { goalHref } from '@/lib/shell/consoleRoutes'
import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { fetchEventsAfter, fetchLatestEvents, type RuntimeEventRow } from '@/api/agentRuntimeEvents'
import { fetchReleaseRecords } from '@/api/releases'
import { RELEASE_RECORD_LIMIT } from '@/pages/shell/releases/releaseView'
import { clockTime } from '../owner/ownerModel'
import { earliestAt, mergeEvents, releasesFrom, taskTitles, timelineRows, trimEvents, TIMELINE_KEEP, TIMELINE_PAGE, TIMELINE_TYPES, type TimelineKind } from './timelineModel'

/** A read on stops after this many full pages; the next poll goes on from where it stopped. */
const READ_ON_PAGES = 5
const TONE: Partial<Record<TimelineKind, string>> = {
  alert: 'var(--color-danger)', failed: 'var(--color-danger)', rejected: 'var(--color-warning)', stop: 'var(--color-warning)', wait: 'var(--color-warning)',
  accepted: 'var(--color-success)', step: 'var(--color-success)', release: 'var(--color-info)',
}
/** What is on screen: `seq` orders what the forward and the backward reads published. */
type Shown = { seq: number; events: RuntimeEventRow[]; complete: boolean; releasesFrom: number; trimmed: boolean }
const message = (e: unknown) => e instanceof Error ? e.message : 'Request failed'

/**
 * The latest events first, then on from the cursor each read gives (`next`), each event kept once by
 * its id; back from the oldest event a latest or back read listed, never from one read on (an event
 * read on may have a lower id than events shown before it). The forward reads are the query: its error
 * and its last success are theirs alone, so a backward read publishes what it found without clearing a
 * failed refresh (review 411 A1).
 *
 * What reading on keeps is a window of the newest TIMELINE_KEEP shown events, cut after every page so a
 * page that fails next cannot carry it past that (review 413 B2-a); what was let go is behind Show older
 * again, whose cursor follows the window's lower edge. What Show older loaded is kept apart and shown
 * with the window, so later reading on does not push it out (review 413 B2-b).
 */
function useTimeline() {
  const kept = useRef({ forward: new Map<number, RuntimeEventRow>(), older: new Map<number, RuntimeEventRow>(), cursor: '', back: 0,
    complete: false, rawFrom: Infinity, trimmed: false, seq: 0 })
  const [backward, setBackward] = useState<{ pending: boolean; error: string; shown: Shown | null }>({ pending: false, error: '', shown: null })
  const publish = (): Shown => {
    const s = kept.current
    const events = [...new Map([...s.older, ...s.forward]).values()]
    return { seq: ++s.seq, events, complete: s.complete, releasesFrom: releasesFrom(s.rawFrom, events, s.complete), trimmed: s.trimmed }
  }
  const query = useQuery({ queryKey: ['agent-runtime-timeline'], retry: false, gcTime: 0, refetchInterval: 15_000,
    queryFn: async ({ signal }) => {
      const s = kept.current
      if (!s.cursor) {
        const page = await fetchLatestEvents({ types: TIMELINE_TYPES, limit: TIMELINE_PAGE }, signal)
        mergeEvents(s.forward, page.events)
        s.back = page.events.at(-1)?.event_id ?? 0
        s.complete = page.events.length < TIMELINE_PAGE
        // How far back the raw page reaches, whatever of it is shown (review 411 B1).
        s.rawFrom = earliestAt(page.events)
        s.cursor = page.next
      } else {
        for (let i = 0; i < READ_ON_PAGES; i++) {
          const page = await fetchEventsAfter(s.cursor, { types: TIMELINE_TYPES, limit: TIMELINE_PAGE }, signal)
          mergeEvents(s.forward, page.events)
          s.cursor = page.next
          const edge = trimEvents(s.forward, TIMELINE_KEEP)
          if (edge !== null) {
            s.back = Math.max(s.back, edge)
            s.complete = false
            s.trimmed = true
          }
          if (page.events.length < TIMELINE_PAGE) break
        }
      }
      return publish()
    } })
  async function readOlder() {
    const s = kept.current
    if (backward.pending || s.complete || !s.back) return
    const from = s.back
    setBackward(b => ({ ...b, pending: true, error: '' }))
    try {
      const page = await fetchLatestEvents({ types: TIMELINE_TYPES, limit: TIMELINE_PAGE, before: from })
      // Reading on moved the window meanwhile: this page may not join what is kept; read again from there.
      if (s.back !== from) { setBackward(b => ({ ...b, pending: false })); return }
      mergeEvents(s.older, page.events)
      s.back = page.events.at(-1)?.event_id ?? s.back
      s.complete = page.events.length < TIMELINE_PAGE
      s.rawFrom = Math.min(s.rawFrom, earliestAt(page.events))
      setBackward({ pending: false, error: '', shown: publish() })
    } catch (e) { setBackward(b => ({ ...b, pending: false, error: message(e) })) }
  }
  const forward = query.data
  const shown = forward && (!backward.shown || forward.seq > backward.shown.seq) ? forward : backward.shown ?? undefined
  return { query, shown, backward, readOlder }
}

/** History's timeline: from the runtime's events and the platform's release records, newest first. */
export function ProgressTimeline({ runtime, now }: { runtime: RuntimeProgress | undefined; now: number }) {
  const { query, shown, backward, readOlder } = useTimeline()
  const releasesQ = useQuery({ queryKey: ['releases', 'records', RELEASE_RECORD_LIMIT], queryFn: () => fetchReleaseRecords(RELEASE_RECORD_LIMIT), refetchInterval: 30_000 })
  const rows = shown ? timelineRows(shown.events, releasesQ.data?.records ?? [], { titles: taskTitles(runtime), releasesFrom: shown.releasesFrom }) : []
  return <section aria-label="Timeline" className="flex min-w-0 flex-col gap-2">
    <h4 className="m-0">Timeline</h4>
    {query.isPending && !shown && <p role="status" className="m-0 text-sm">Reading the runtime's events…</p>}
    {query.isError && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">The runtime's events could not be read: {query.error.message}
      {query.dataUpdatedAt > 0 ? `. The events below were read at ${clockTime(query.dataUpdatedAt, now)}; newer ones are not shown.` : ''}</p>}
    {releasesQ.isError && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Release records could not be read: {releasesQ.error.message}</p>}
    {shown && rows.length === 0 && <p className="m-0 text-sm text-muted-foreground">{shown.complete ? 'Nothing has happened yet.' : 'Nothing to show among the latest events; read older ones.'}</p>}
    <ol aria-label="Timeline" className="m-0 flex list-none flex-col gap-1.5 p-0">{rows.map(row => <li key={row.key} data-kind={row.kind} className="flex min-w-0 flex-col gap-0.5 text-sm">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-2">
        <time dateTime={row.at} title={row.at} className="shrink-0 font-mono text-xs text-muted-foreground">{clockTime(Date.parse(row.at), now)}</time>
        <span className="shrink-0 font-semibold" style={{ color: TONE[row.kind] }}>{row.label}</span>
        <span className="min-w-0 [overflow-wrap:anywhere]">{row.text}</span>
        {row.mission && <a href={goalHref(row.mission)} className="shrink-0 text-xs">{row.mission}</a>}
      </div>
      {row.detail && <span className="min-w-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">{row.detail}</span>}
    </li>)}</ol>
    {shown?.trimmed && !shown.complete && <p className="m-0 text-xs text-muted-foreground">Only the newest events are kept on this page; older ones are behind Show older.</p>}
    {backward.error && <p role="alert" className="m-0 text-sm [overflow-wrap:anywhere]">Older events could not be read: {backward.error}</p>}
    {shown && !shown.complete && <button type="button" disabled={backward.pending} onClick={() => { void readOlder() }}
      className="min-h-11 self-start text-sm underline disabled:opacity-60">{backward.pending ? 'Reading older events…' : 'Show older'}</button>}
  </section>
}
