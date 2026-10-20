import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { AGENT_PROGRESS_POLL_MS } from '@/api/agentProgress'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { PLAN_ROOT, fetchImportRequest, fetchRuntimePlan, runtimeRound, type PlanAnswer, type RuntimeRound } from '@/api/agentRuntimePlan'
import { fromPlanWithRequest, type PlanRequest } from '@/api/agentRuntimePlanView'

/** The one line a plan pane shows while it reads the board. */
export const PLAN_FROM_BOARD = 'Plan not in the runtime yet; read from the board.'

/**
 * Where the plan panes read the plan from (2.5 slice 2): the runtime once it holds the round, the
 * board only while the runtime answers that it holds no such root (404: not imported yet). A read that
 * fails is an error, never the board's numbers.
 */
export type PlanRead =
  | { from: 'loading' }
  | { from: 'board' }
  | { from: 'runtime'; round: RuntimeRound }
  | { from: 'error'; error: string }

type Read<T> = { data: T | undefined; error: unknown; isError: boolean; isPaused: boolean }
const failure = (q: Read<unknown>) => q.isError ? (q.error instanceof Error ? q.error.message : 'Request failed') : q.isPaused ? 'offline' : null

/** The plan from the three reads: GET /plan?root=, the import request its head names, and GET /progress. */
export function planRead(plan: Read<PlanAnswer>, request: Read<PlanRequest | null>, progress: Read<RuntimeProgress>): PlanRead {
  const failed = failure(plan)
  if (failed !== null) return { from: 'error', error: failed }
  if (plan.data === undefined) return { from: 'loading' }
  if (!plan.data.found) return { from: 'board' }
  const other = failure(progress) ?? failure(request)
  if (other !== null) return { from: 'error', error: other }
  if (progress.data === undefined || request.data === undefined) return { from: 'loading' }
  try {
    return { from: 'runtime', round: runtimeRound(fromPlanWithRequest(plan.data.doc, PLAN_ROOT, request.data), progress.data.plan, PLAN_ROOT, request.data) }
  } catch (e) {
    return { from: 'error', error: e instanceof Error ? e.message : String(e) }
  }
}

/**
 * The plan for a pane. GET /progress is read under the key Needs you reads it with; the request is
 * content-addressed and read once, again only after a failed read.
 */
export function useRuntimePlan(enabled = true): PlanRead {
  const planQ = useQuery({ queryKey: ['agent-runtime-plan', PLAN_ROOT], queryFn: ({ signal }) => fetchRuntimePlan(PLAN_ROOT, signal),
    enabled, refetchInterval: AGENT_PROGRESS_POLL_MS, retry: false })
  const requestQ = useQuery({ queryKey: ['agent-runtime-plan-request', PLAN_ROOT], queryFn: ({ signal }) => fetchImportRequest(PLAN_ROOT, signal),
    enabled: enabled && planQ.data?.found === true, staleTime: Infinity, retry: false,
    refetchInterval: q => q.state.status === 'error' ? AGENT_PROGRESS_POLL_MS : false })
  const progressQ = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal),
    enabled, refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const { data: plan, error: planError, isError: planFailed, isPaused: planPaused } = planQ
  const { data: request, error: requestError, isError: requestFailed, isPaused: requestPaused } = requestQ
  const { data: progress, error: progressError, isError: progressFailed, isPaused: progressPaused } = progressQ
  return useMemo(() => planRead(
    { data: plan, error: planError, isError: planFailed, isPaused: planPaused },
    { data: request, error: requestError, isError: requestFailed, isPaused: requestPaused },
    { data: progress, error: progressError, isError: progressFailed, isPaused: progressPaused },
  ), [plan, planError, planFailed, planPaused, request, requestError, requestFailed, requestPaused, progress, progressError, progressFailed, progressPaused])
}
