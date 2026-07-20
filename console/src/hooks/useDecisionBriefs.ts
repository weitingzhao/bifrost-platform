import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  decideOnBrief,
  fetchDecisionBriefs,
  OPERATE_BRIEFS_QUERY_KEY,
  OPERATE_DRAIN_STATUS_QUERY_KEY,
  type BriefDecision,
  type DecisionBrief,
} from '@/api/operateBriefs'
import { OPERATE_QUEUE_QUERY_KEY } from '@/api/operateQueue'

/** Actionable briefs: undecided, or active hold (owner can re-decide). */
export function isPendingDecisionBrief(brief: DecisionBrief): boolean {
  const decision = brief.decision
  if (decision == null || decision === '') return true
  if (decision === 'hold') {
    const until = brief.hold_until?.trim()
    if (until == null || until === '') return true
    const ms = Date.parse(until)
    return Number.isFinite(ms) && ms > Date.now()
  }
  return false
}

export function isHeldDecisionBrief(brief: DecisionBrief): boolean {
  return brief.decision === 'hold' && isPendingDecisionBrief(brief)
}

export function useDecisionBriefs(options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: OPERATE_BRIEFS_QUERY_KEY,
    queryFn: fetchDecisionBriefs,
    refetchInterval: 30_000,
    enabled: options?.enabled ?? true,
  })
}

/** Pending briefs only (no Owner decision yet). */
export function usePendingDecisionBriefs(options?: { enabled?: boolean }) {
  const query = useDecisionBriefs(options)
  const pending = useMemo(
    () => (query.data ?? []).filter(isPendingDecisionBrief),
    [query.data],
  )
  return { ...query, pending, pendingCount: pending.length }
}

export function useDecideOnBrief() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, decision }: { id: string; decision: BriefDecision }) =>
      decideOnBrief(id, decision),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: OPERATE_BRIEFS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: OPERATE_QUEUE_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: OPERATE_DRAIN_STATUS_QUERY_KEY })
      void queryClient.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })
}
