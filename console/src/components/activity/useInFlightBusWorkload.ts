import { useActivityFeed } from '@/lib/activity/activityStore'
import {
  isActivityInFlight,
  parseActivityTarget,
} from '@/lib/activity/activityPageFocus'
import type { ActivityEvent } from '@/lib/activity/activityTypes'

export function matchesNamespace(ev: ActivityEvent, namespace: string): boolean {
  if (ev.linkTo != null && ev.linkTo !== 'satellite-bus') return false
  if (ev.kind !== 'actuation') return false
  const { namespace: ns } = parseActivityTarget(ev.target)
  if (ns != null) return ns === namespace
  return ev.target?.includes(namespace) === true || ev.id.includes(namespace)
}

/** In-flight actuation workload for the given namespace (for row highlight). */
export function useInFlightBusWorkload(namespace: string): string | null {
  const { events } = useActivityFeed()
  const ev = events.find(e => matchesNamespace(e, namespace) && isActivityInFlight(e))
  if (ev == null) return null
  return parseActivityTarget(ev.target).workload ?? null
}
