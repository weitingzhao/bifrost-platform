import type { ActivityEvent } from './activityTypes'

export function parseActivityTarget(
  target: string | undefined,
): { namespace?: string; workload?: string } {
  if (target == null || target === '') return {}
  const parts = target.split('/').filter(Boolean)
  if (parts.length >= 2) {
    return { namespace: parts[0], workload: parts[parts.length - 1] }
  }
  return { workload: parts[0] }
}

export function isActivityInFlight(ev: ActivityEvent): boolean {
  return ev.phase === 'requested' || ev.phase === 'applying'
}
