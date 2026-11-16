import { changeText, changeRef, clock } from '@/domains/plan/readings/recentChanges'
import { useState } from 'react'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { fetchPlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'

/** How many changes show before "Show n more" (Design v3.6 WorkPage). */
const FIRST_SHOWN = 8
/** How many changes the plan view returns per page (store/planviewrecent.go: LIMIT 21, the 21st only says there are more). */
const RECENT_PAGE = 20
const caps = 'text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const mono = 'font-mono text-xs text-muted-foreground'

/**
 * Work → Changes (Design v3.6 WorkPage): the plan's recent changes, newest first, eight at a time. "8 of 20 read" says
 * how many of the changes read so far show; the plan view returns twenty a page, so twenty is a page, not every change:
 * older pages load on request while it says there are more. History keeps the server's fixed boundary while the live
 * view polls; Latest changes reads from the newest again.
 */
export function PlanRecentChanges({ view }: { view: PlanSnapshot }) {
  const client = useQueryClient()
  const [shown, setShown] = useState(FIRST_SHOWN)
  const history = useInfiniteQuery({
    queryKey: ['agent-runtime-plan-view-history', view.root], initialPageParam: '',
    queryFn: ({ signal, pageParam }) => fetchPlanSnapshot(view.root, signal, pageParam),
    getNextPageParam: page => page.recent_next || undefined,
    initialData: { pages: [view], pageParams: [''] }, staleTime: Infinity, retry: false,
  })
  const pages = history.data.pages
  const seen = new Set<number>()
  const events = pages.flatMap(p => p.recent_changes).filter(e => { if (seen.has(e.event_id)) return false; seen.add(e.event_id); return true })
  const visible = events.slice(0, shown)
  const now = Date.now()
  const latestNewer = pages.length > 1 || pages[0].recent_as_of !== view.recent_as_of
  return <section aria-label="Recent changes" className="flex min-w-0 flex-col gap-1 rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)]">
    <div className="flex flex-wrap items-center justify-between gap-2 pb-1.5">
      <h3 className={`m-0 ${caps}`}>Recent changes · {visible.length} of {events.length} read</h3>
      <span className={mono}>plan-view {view.root} · recent_changes · History snapshot · {pages[0].recent_as_of}</span>
    </div>
    <p className="m-0 pb-1 text-xs text-muted-foreground">The plan view returns {RECENT_PAGE} changes a page{history.hasNextPage ? '; older ones load on request.' : '; it holds no older ones.'}</p>
    {events.length === 0 && <p className="m-0 text-sm">No recorded changes in this snapshot.</p>}
    <ol className="m-0 flex list-none flex-col p-0">{visible.map(event => {
      const ref = changeRef(event, view)
      return <li key={event.event_id} className="grid grid-cols-[4.5rem_0.75rem_minmax(0,1fr)] items-start gap-2 border-b border-[var(--table-rule)] py-1.5 [overflow-wrap:anywhere]">
        <time dateTime={event.at} title={event.at} className={`${mono} pt-0.5`}>{clock(event.at, now)}</time>
        <span aria-hidden className="mt-1.5 size-2 rounded-full bg-[var(--primary)]" />
        <div className="min-w-0 text-sm leading-[1.45]">
          {changeText(event)}{ref && <span className={mono}> {ref}</span>} <span className={mono}>· {event.event_type} · {event.actor_id}</span>
          <details><summary className="cursor-pointer text-xs text-muted-foreground pointer-coarse:min-h-11 pointer-coarse:py-3">Event #{event.event_id} · Evidence</summary><pre className="whitespace-pre-wrap text-xs">{JSON.stringify(event.body_json, null, 2)}</pre></details>
        </div>
      </li>
    })}</ol>
    {history.isError && <p role="alert">Recent changes Unknown · {history.error.message}</p>}
    <div className="flex flex-wrap gap-x-4">
      {visible.length < events.length && <button type="button" className="min-h-11 border-0 bg-transparent p-0 text-sm font-semibold text-[var(--primary)]" onClick={() => setShown(events.length)}>Show {events.length - visible.length} more</button>}
      {visible.length === events.length && history.hasNextPage && <button type="button" className="min-h-11 border-0 bg-transparent p-0 text-sm font-semibold text-[var(--primary)]" disabled={history.isFetchingNextPage}
        onClick={() => { setShown(Infinity); void history.fetchNextPage() }}>{history.isFetchingNextPage ? 'Loading changes…' : 'Load more changes'}</button>}
      {latestNewer && <button type="button" className="min-h-11 border-0 bg-transparent p-0 text-sm font-semibold text-[var(--primary)]" disabled={history.isFetching}
        onClick={() => { setShown(FIRST_SHOWN); client.setQueryData(['agent-runtime-plan-view-history', view.root], { pages: [view], pageParams: [''] }) }}>Latest changes</button>}
    </div>
  </section>
}
