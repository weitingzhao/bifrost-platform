import { useQuery } from '@tanstack/react-query'
import { DeliveryLanes } from '@bifrost/ui/city/lanes'
import { engineQueryKey } from '@/api/engine'
import { fetchSelfHealth } from '@/api/core'
import { useHashParam } from '@/lib/shell/useHashSection'
import { deliveryHref, ENGINE_LANES_PATH, engineLanesOn, fetchLanes, LANES_LIMIT, lanesLine, lineInCity, lineLanesHref, OPEN_IN_CITY } from './deliveryLanesModel'

/**
 * Work → Delivery: one lane per change from its branch to PROD, the stuck ones first, a picked lane in the address
 * (`#work?section=delivery&line=<Change-Id>`). Through the engine only; where it is off the page says so.
 */
export function DeliveryLanePage() {
  const health = useQuery({ queryKey: ['shell', 'self-health'], queryFn: () => fetchSelfHealth(), staleTime: 10_000 })
  const on = engineLanesOn(health.data?.viewer_env)
  const q = useQuery({
    queryKey: engineQueryKey(ENGINE_LANES_PATH, { limit: String(LANES_LIMIT) }),
    queryFn: ({ signal }) => fetchLanes(signal),
    enabled: on,
    refetchInterval: 30_000,
    retry: false,
  })
  const picked = useHashParam('line') ?? ''
  // One line's lanes (the shared context's line ↔ lane, 箭10 2026-11-25): session=<thread>.
  const session = useHashParam('session') ?? ''
  const all = q.data?.lanes ?? []
  const lanes = session ? all.filter(l => l.session === session) : all
  const pickedLane = all.find(l => l.id === picked)
  const lineName = (s: string) => all.find(l => l.session === s)?.title ?? s
  const env = health.data?.viewer_env
  return (
    <div className="flex min-w-0 flex-col gap-3" data-delivery-lanes>
      <div className="flex flex-wrap items-baseline justify-between gap-2 text-sm">
        <span data-lanes-line>{on && q.data ? lanesLine({ lanes }) : on && q.isError ? lanesLine(null) : ''}</span>
        <a href={OPEN_IN_CITY} className="inline-flex min-h-11 items-center underline">Open in City</a>
      </div>
      {health.isLoading ? <p className="m-0 text-sm text-muted-foreground">Reading…</p> : null}
      {!health.isLoading && !on ? (
        <p className="m-0 text-sm" data-lanes-off>
          {`boundary: the delivery lanes come only through the UI engine, which is not switched on here (${env ?? 'environment unknown'}). There is no direct read to fall back to.`}
        </p>
      ) : null}
      {session ? (
        <p className="m-0 flex flex-wrap items-center gap-x-3 text-sm" data-lanes-session>
          <span>{`Lanes of ${lineName(session)}`}</span>
          <a href={deliveryHref()} className="inline-flex min-h-11 items-center underline">Show all</a>
          <a href={lineInCity(session)} className="inline-flex min-h-11 items-center underline">This line in City</a>
        </p>
      ) : null}
      {pickedLane?.session ? (
        <p className="m-0 flex flex-wrap items-center gap-x-3 text-sm" data-lane-line>
          <span>{`Picked lane belongs to ${lineName(pickedLane.session)}`}</span>
          <a href={lineLanesHref(pickedLane.session)} className="inline-flex min-h-11 items-center underline">This line</a>
        </p>
      ) : null}
      {on && q.isError ? <p className="m-0 text-sm" role="alert">{q.error instanceof Error ? q.error.message : String(q.error)}</p> : null}
      {on && !q.isError ? (
        <DeliveryLanes
          lanes={lanes}
          sources={q.data?.sources}
          oldestRun={q.data?.oldestRun}
          reading={q.isLoading}
          selected={picked}
          onSelect={id => { window.location.hash = deliveryHref(id || undefined, session || undefined) }}
          scope={`the last ${LANES_LIMIT} changes and every stuck one, through the engine (${ENGINE_LANES_PATH})`}
        />
      ) : null}
    </div>
  )
}
