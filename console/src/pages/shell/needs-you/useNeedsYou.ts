import { useQuery } from '@tanstack/react-query'
import { fetchApprovalList, isAwaitingDecision } from '@/api/approvals'
import type { NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'

export const NEEDS_YOU_REFRESH_MS = 30_000

/** Pending approvals, shared by the sidebar count and the Needs you page. */
export function useNeedsYou() {
  const q = useQuery({
    queryKey: ['approvals', 'pending'],
    queryFn: () => fetchApprovalList('pending'),
    refetchInterval: NEEDS_YOU_REFRESH_MS,
    retry: false,
  })
  const toApprove = (q.data ?? []).filter(item => isAwaitingDecision(item))
  let count: NeedsYouCount
  if (q.isError) {
    count = {
      state: 'unknown',
      reason: q.error instanceof Error ? q.error.message : 'approvals request failed',
    }
  } else if (q.data == null) {
    count = { state: 'loading' }
  } else {
    count = { state: 'known', count: toApprove.length }
  }
  return { count, toApprove, query: q }
}
