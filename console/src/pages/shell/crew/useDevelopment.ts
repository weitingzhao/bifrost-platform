import { useQuery } from '@tanstack/react-query'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress } from '@/api/agentRuntime'
import { fetchImportRequest, fetchRuntimePlan } from '@/api/agentRuntimePlan'
import { planRead } from '../agents/useRuntimePlan'
import { developmentRead } from './developmentModel'

/** Reuses Work's query keys and parsers; no separate progress ledger. */
export function useDevelopment(now: number) {
  const progress = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const plan = useQuery({ queryKey: ['agent-runtime-plan', 'R4'], queryFn: ({ signal }) => fetchRuntimePlan('R4', signal), refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const request = useQuery({ queryKey: ['agent-runtime-plan-request', 'R4'], queryFn: ({ signal }) => fetchImportRequest('R4', signal), enabled: plan.data?.found === true,
    staleTime: Infinity, retry: false, refetchInterval: q => q.state.status === 'error' ? AGENT_RUNTIME_POLL_MS : false })
  const read = developmentRead(progress, planRead(plan, request, progress, 'R4'), plan.dataUpdatedAt, now)
  const refresh = () => { void progress.refetch(); void plan.refetch(); if (plan.data?.found) void request.refetch() }
  return { read, refresh, refreshing: progress.isFetching || plan.isFetching || request.isFetching }
}
