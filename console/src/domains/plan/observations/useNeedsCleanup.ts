import type { UseQueryResult } from '@tanstack/react-query'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { needsCleanup, type NeedsCleanup } from './needsCleanupModel'
import type { PlanAddress } from '@/pages/shell/agents/usePlanSnapshot'
import { workSectionHref } from '@/pages/shell/agents/workViewModel'

/**
 * Work's Needs cleanup list, from the shown round's plan view Work already holds.
 */
export function useNeedsCleanup(plan: PlanAddress, query: UseQueryResult<PlanSnapshot>, now: number): NeedsCleanup {
  const failed = query.isPaused ? 'Offline' : query.isError ? query.error?.message ?? 'Request failed' : null
  return needsCleanup({ view: query.data, failed, root: plan.root, rounds: plan.rounds }, now)
}

/** Where the list is: Work → Activity, on the round the address names (or the newest). */
export const needsCleanupHref = (plan: PlanAddress) => workSectionHref('activity', plan.from === 'address' ? plan.root : null)
