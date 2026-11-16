import { useQuery } from '@tanstack/react-query'
import { fetchPlanSnapshot } from '@/api/planSnapshot'

/** One round's plan view; nothing is read while the round is not known. */
export function usePlanSnapshot(root: string | null, enabled = true) {
  return useQuery({ enabled: enabled && root !== null, queryKey: ['agent-runtime-plan-view', root ?? ''], queryFn: ({ signal }) => fetchPlanSnapshot(root ?? '', signal),
    refetchInterval: 15_000, retry: false })
}

