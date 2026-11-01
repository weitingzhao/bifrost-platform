import { useQuery, type UseQueryResult } from '@tanstack/react-query'
import { fetchRuntimeProgress } from '@/api/agentRuntime'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { needsCleanup, type NeedsCleanup } from './needsCleanupModel'
import type { PlanAddress } from './usePlanSnapshot'
import { workSectionHref } from './workViewModel'

/**
 * Work's Needs cleanup list, from the reads Work already holds: GET /progress as the shell polls it (read from its cache:
 * no request of its own unless nothing has read it) and the shown round's plan view.
 */
export function useNeedsCleanup(plan: PlanAddress, query: UseQueryResult<PlanSnapshot>, now: number): NeedsCleanup {
  const progress = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), staleTime: Infinity, retry: false })
  const failed = query.isPaused ? 'Offline' : query.isError ? query.error?.message ?? 'Request failed' : null
  return needsCleanup(progress, { view: query.data, failed, root: plan.root, rounds: plan.rounds }, now)
}

/** Where the list is: Work → Activity, on the round the address names (or the newest). */
export const needsCleanupHref = (plan: PlanAddress) => workSectionHref('activity', plan.from === 'address' ? plan.root : null)
