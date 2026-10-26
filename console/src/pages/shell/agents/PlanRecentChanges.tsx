import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { fetchPlanSnapshot, type PlanSnapshot } from '@/api/planSnapshot'
import { Card } from '../owner/OwnerParts'

/** History keeps the server's fixed boundary while the live view polls. */
export function PlanRecentChanges({ view }: { view: PlanSnapshot }) {
  const client = useQueryClient()
  const history = useInfiniteQuery({
    queryKey: ['agent-runtime-plan-view-history', view.root], initialPageParam: '',
    queryFn: ({ signal, pageParam }) => fetchPlanSnapshot(view.root, signal, pageParam),
    getNextPageParam: page => page.recent_next || undefined,
    initialData: { pages: [view], pageParams: [''] }, staleTime: Infinity, retry: false,
  })
  const pages = history.data.pages
  const seen = new Set<number>()
  const events = pages.flatMap(p => p.recent_changes).filter(e => { if (seen.has(e.event_id)) return false; seen.add(e.event_id); return true })
  return <Card label="Recent changes" title="Recent changes">
    <p className="m-0 text-xs">History snapshot · {pages[0].recent_as_of}</p>
    <button className="min-h-11 self-start rounded border px-3" disabled={history.isFetching} onClick={() => client.setQueryData(['agent-runtime-plan-view-history', view.root], { pages: [view], pageParams: [''] })}>Latest changes</button>
    {events.length === 0 && <p>No recorded changes in this snapshot.</p>}
    <ol className="m-0 flex list-none flex-col gap-2 p-0">{events.map(event => <li key={event.event_id} className="border-b border-[var(--table-rule)] py-2 [overflow-wrap:anywhere]">
      <strong>{event.event_type}</strong> · {event.actor_id} · <time>{event.at}</time>
      <details><summary className="min-h-11 cursor-pointer py-3">Event #{event.event_id} · Evidence</summary><pre className="whitespace-pre-wrap text-xs">{JSON.stringify(event.body_json, null, 2)}</pre></details>
    </li>)}</ol>
    {history.isError && <p role="alert">Recent changes Unknown · {history.error.message}</p>}
    {history.hasNextPage && <button className="min-h-11 self-start rounded border px-3" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{history.isFetchingNextPage ? 'Loading changes…' : 'Load more changes'}</button>}
  </Card>
}
