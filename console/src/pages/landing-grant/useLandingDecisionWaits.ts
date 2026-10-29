import { useQueries } from '@tanstack/react-query'
import type { ApprovalItem } from '@/api/approvals'
import { fetchLandingWait, isLandingAction } from '@/api/landingGrant'

/** §5.8: a stale request or an already-authorized operation is not an Owner decision. */
export function useLandingDecisionWaits(items: readonly ApprovalItem[]) {
  const landing = items.filter(item => item.status === 'pending' && isLandingAction(item.action) && !item.drill)
  const ops = [...new Set(landing.map(item => typeof item.params?.op === 'string' ? item.params.op : ''))]
  const reads = useQueries({ queries: ops.map(op => ({ queryKey: ['landing-wait', op], queryFn: ({ signal }: { signal: AbortSignal }) => fetchLandingWait(op, signal), enabled: op !== '', retry: false, refetchInterval: 15_000 })) })
  const eligible = new Set<string>()
  let unreadable = false, loading = false
  for (const item of landing) {
    const op = typeof item.params?.op === 'string' ? item.params.op : ''
    const q = reads[ops.indexOf(op)]
    if (!op || q.isError || q.isPaused) { unreadable = true; continue }
    if (q.isPending) { loading = true; continue }
    const w = q.data
    if (!w || (w.phase === 'awaiting_grant' && !w.main_known)) { unreadable = true; continue }
    if (w.owner_todo && w.approval_id === item.id && w.approval_number === item.number) eligible.add(item.id)
  }
  return { eligible, unreadable, loading }
}
