import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { AGENT_PROGRESS_POLL_MS } from '@/api/agentProgress'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { PLAN_ROOT, approvalsByStep, fetchImportRequest, fetchRuntimePlan, runtimeArch, runtimeRound, signoffsByStep, stepsOfApproval, type ApprovalStep, type PlanAnswer, type RuntimeArchTable, type RuntimeRound } from '@/api/agentRuntimePlan'
import { fromPlanWithRequest, type PlanRequest } from '@/api/agentRuntimePlanView'
import { record } from '@/api/agentRuntime'

/** The one line a plan pane shows while the runtime holds no plan. */
export const PLAN_NOT_IMPORTED = 'The plan is not in the agent runtime here yet.'

/** The one line an architecture pane shows while the runtime holds no architecture table. */
export const ARCH_NOT_IMPORTED = 'The architecture table is not in the agent runtime here yet.'

/**
 * Where the plan panes read the plan from: the runtime only (2.5 slice 2, and ⑤: the board is retired).
 * absent is the runtime answering that it holds no such root (404: not imported here); a read that
 * fails is an error.
 */
export type PlanRead =
  | { from: 'loading' }
  | { from: 'absent' }
  | { from: 'runtime'; round: RuntimeRound }
  | { from: 'error'; error: string }

/** The architecture table, from GET /progress only (2.5 ⑤ G2). absent: the runtime holds no architecture root. */
export type ArchRead =
  | { from: 'loading' }
  | { from: 'absent' }
  | { from: 'runtime'; table: RuntimeArchTable }
  | { from: 'error'; error: string }

type Read<T> = { data: T | undefined; error: unknown; isError: boolean; isPaused: boolean }
const failure = (q: Read<unknown>) => q.isError ? (q.error instanceof Error ? q.error.message : 'Request failed') : q.isPaused ? 'offline' : null

/** The plan from the three reads: GET /plan?root=, the import request its head names, and GET /progress. */
export function planRead(plan: Read<PlanAnswer>, request: Read<PlanRequest | null>, progress: Read<RuntimeProgress>): PlanRead {
  const failed = failure(plan)
  if (failed !== null) return { from: 'error', error: failed }
  if (plan.data === undefined) return { from: 'loading' }
  if (!plan.data.found) return { from: 'absent' }
  const other = failure(progress) ?? failure(request)
  if (other !== null) return { from: 'error', error: other }
  if (progress.data === undefined || request.data === undefined) return { from: 'loading' }
  try {
    return { from: 'runtime', round: runtimeRound(fromPlanWithRequest(plan.data.doc, PLAN_ROOT, request.data), progress.data.plan, PLAN_ROOT, request.data,
      approvalsByStep(plan.data.doc), signoffsByStep(plan.data.doc)) }
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

/** The architecture table from one read of GET /progress: a failed or paused read is an error, never an empty table. */
export function archRead(progress: Read<RuntimeProgress>): ArchRead {
  const failed = failure(progress)
  if (failed !== null) return { from: 'error', error: failed }
  if (progress.data === undefined) return { from: 'loading' }
  // An answer with no plan list is not one that holds no architecture root (review 431 B1).
  if (progress.data.planRead === false) return { from: 'error', error: 'Agent runtime: the progress answer has no plan list' }
  try {
    const table = runtimeArch(progress.data.plan)
    return table ? { from: 'runtime', table } : { from: 'absent' }
  } catch (e) {
    return { from: 'error', error: e instanceof Error ? e.message : String(e) }
  }
}

/** The architecture table for a pane, from GET /progress under the key Needs you and the plan read it with. */
export function useRuntimeArch(enabled = true): ArchRead {
  const progressQ = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal),
    enabled, refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const { data, error, isError, isPaused } = progressQ
  return useMemo(() => archRead({ data, error, isError, isPaused }), [data, error, isError, isPaused])
}

/**
 * Which steps of the round a request is the approval for (v4 §20.5 scenario 10), from the same GET /plan?root= read the
 * plan panes make: read is 'ok' with the steps (none when the plan holds none, or holds no such root), or the read is not
 * done yet or failed, and nothing is said.
 */
export type ApprovalStepsRead = { read: 'ok'; stepsOf: (n: number | undefined) => ApprovalStep[] } | { read: 'loading' | 'error' }
export function useApprovalSteps(enabled = true): ApprovalStepsRead {
  const planQ = useQuery({ queryKey: ['agent-runtime-plan', PLAN_ROOT], queryFn: ({ signal }) => fetchRuntimePlan(PLAN_ROOT, signal),
    enabled, refetchInterval: AGENT_PROGRESS_POLL_MS, retry: false })
  const { data, isError, isPaused } = planQ
  return useMemo((): ApprovalStepsRead => {
    if (isError || isPaused) return { read: 'error' }
    if (data === undefined) return { read: 'loading' }
    const doc = data.found ? data.doc : null
    // An answer that is not this round's tree is a failed read, never one with no steps (review 476 C2).
    if (doc != null && (record(doc).root !== PLAN_ROOT || !Array.isArray(record(doc).nodes))) return { read: 'error' }
    return { read: 'ok', stepsOf: n => doc == null || n == null || n <= 0 ? [] : stepsOfApproval(doc, n) }
  }, [data, isError, isPaused])
}
