import { useQuery } from '@tanstack/react-query'
import { hostsNeedingYou, silentThreads } from '@/api/agentThreads'
import { fetchApprovalList, isAwaitingDecision } from '@/api/approvals'
import { useAgentThreads } from '@/hooks/useAgentThreads'
import type { NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'

export const NEEDS_YOU_REFRESH_MS = 30_000

function reasonOf(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

/**
 * Pending approvals and silent agent threads (W-54), shared by the sidebar count
 * and the Needs you page. The total is Unknown when either source cannot be read.
 */
export function useNeedsYou() {
  const q = useQuery({
    queryKey: ['approvals', 'pending'],
    queryFn: () => fetchApprovalList('pending'),
    refetchInterval: NEEDS_YOU_REFRESH_MS,
    retry: false,
  })
  const threadsQuery = useAgentThreads()
  const toApprove = (q.data ?? []).filter(item => isAwaitingDecision(item))
  const silent = silentThreads(threadsQuery.data?.threads ?? [])
  const lost = hostsNeedingYou(threadsQuery.data?.hosts)

  let approveCount: NeedsYouCount
  if (q.isError) {
    approveCount = { state: 'unknown', reason: reasonOf(q.error, 'approvals request failed') }
  } else if (q.data == null) {
    approveCount = { state: 'loading' }
  } else {
    approveCount = { state: 'known', count: toApprove.length }
  }

  let silentCount: NeedsYouCount
  if (threadsQuery.isError) {
    silentCount = { state: 'unknown', reason: reasonOf(threadsQuery.error, 'agent threads request failed') }
  } else if (threadsQuery.data == null) {
    silentCount = { state: 'loading' }
  } else {
    silentCount = { state: 'known', count: silent.length + lost.length }
  }

  let count: NeedsYouCount
  if (approveCount.state === 'unknown') count = approveCount
  else if (silentCount.state === 'unknown') count = silentCount
  else if (approveCount.state === 'loading' || silentCount.state === 'loading') count = { state: 'loading' }
  else count = { state: 'known', count: approveCount.count + silentCount.count }

  return { count, approveCount, silentCount, toApprove, silent, lost, query: q, threadsQuery }
}
