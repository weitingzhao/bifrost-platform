import { useQueries, useQuery, type UseQueryResult } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { resolveConsoleHash } from '@/lib/shell/consoleRoutes'
import { AGENT_PROGRESS_POLL_MS } from '@/api/agentProgress'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress, type RuntimeProgress } from '@/api/agentRuntime'
import { PLAN_ROOT, approvalsByStep, currentRound, fetchImportRequest, fetchRuntimePlan, planRounds, runtimeArch, runtimeRound, signoffNotStarted, signoffWaits, signoffsByStep, stepsOfApproval, type ApprovalStep, type PlanAnswer, type RuntimeArchTable, type RuntimeRound, type SignoffWait } from '@/api/agentRuntimePlan'
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
  | { from: 'error'; error: string; readAt?: number | null }

/** The architecture table, from GET /progress only (2.5 ⑤ G2). absent: the runtime holds no architecture root. */
export type ArchRead =
  | { from: 'loading' }
  | { from: 'absent' }
  | { from: 'runtime'; table: RuntimeArchTable }
  | { from: 'error'; error: string; readAt?: number | null }

/** When a read made of several last answered whole: the oldest of their answers; null before each has answered once. */
const wholeAt = (...updatedAt: number[]): number | null => (updatedAt.every(at => at > 0) ? Math.min(...updatedAt) : null)

type Read<T> = { data: T | undefined; error: unknown; isError: boolean; isPaused: boolean }
const failure = (q: Read<unknown>) => q.isError ? (q.error instanceof Error ? q.error.message : 'Request failed') : q.isPaused ? 'offline' : null

/** The plan of round root from the three reads: GET /plan?root=, the import request its head names, and GET /progress. */
export function planRead(plan: Read<PlanAnswer>, request: Read<PlanRequest | null>, progress: Read<RuntimeProgress>, root = PLAN_ROOT): PlanRead {
  const failed = failure(plan)
  if (failed !== null) return { from: 'error', error: failed }
  if (plan.data === undefined) return { from: 'loading' }
  if (!plan.data.found) return { from: 'absent' }
  const other = failure(progress)
  if (other !== null) return { from: 'error', error: other }
  // The import request gives only each step's ledger line and an imported step's date. One that cannot be read leaves
  // the plan, its counts and its sign-offs standing, said without those (2026-10-21: a PROD 404 on it hid the plan and
  // the Owner's Sign off).
  const ledger = failure(request)
  if (progress.data === undefined || (ledger === null && request.data === undefined)) return { from: 'loading' }
  try {
    const req = ledger === null ? request.data ?? null : null
    const round = runtimeRound(fromPlanWithRequest(plan.data.doc, root, req, ledger !== null), progress.data.plan, root, req,
      approvalsByStep(plan.data.doc), signoffsByStep(plan.data.doc))
    return { from: 'runtime', round: ledger === null ? round : { ...round, ledgerUnread: ledger } }
  } catch (e) {
    return { from: 'error', error: e instanceof Error ? e.message : String(e) }
  }
}

/**
 * The round the panes read: the newest GET /progress counts; PLAN_ROOT when that read failed (it then reports the
 * failure itself); null until it has answered.
 */
function roundOf(progress: Read<RuntimeProgress>): string | null {
  if (progress.data !== undefined) return currentRound(progress.data.plan)
  return progress.isError || progress.isPaused ? PLAN_ROOT : null
}

/** Work can name a historical round; other domains continue following the newest round. */
function requestedPlanRoot(): string | null {
  const resolved = resolveConsoleHash(window.location.hash.replace(/^#/, ''))
  if (resolved.location.kind !== 'shell' || resolved.location.id !== 'work') return null
  const root = new URLSearchParams(resolved.canonical.split('?')[1]).get('round')
  return root && /^R[1-9]\d*$/.test(root) ? root : null
}

/**
 * The plan for a pane: the newest round GET /progress counts. GET /progress is read under the key Needs you reads
 * it with; the request is content-addressed and read once, again only after a failed read.
 */
export function useRuntimePlan(enabled = true): PlanRead {
  const progressQ = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal),
    enabled, refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const { data: progress, error: progressError, isError: progressFailed, isPaused: progressPaused } = progressQ
  const [requestedRoot, setRequestedRoot] = useState(requestedPlanRoot)
  useEffect(() => {
    const update = () => setRequestedRoot(requestedPlanRoot())
    window.addEventListener('hashchange', update)
    return () => window.removeEventListener('hashchange', update)
  }, [])
  const root = requestedRoot ?? roundOf({ data: progress, error: progressError, isError: progressFailed, isPaused: progressPaused })
  const planQ = useQuery({ queryKey: ['agent-runtime-plan', root], queryFn: ({ signal }) => fetchRuntimePlan(root ?? PLAN_ROOT, signal),
    enabled: enabled && root !== null, refetchInterval: AGENT_PROGRESS_POLL_MS, retry: false })
  const requestQ = useQuery({ queryKey: ['agent-runtime-plan-request', root], queryFn: ({ signal }) => fetchImportRequest(root ?? PLAN_ROOT, signal),
    enabled: enabled && root !== null && planQ.data?.found === true, staleTime: Infinity, retry: false,
    refetchInterval: q => q.state.status === 'error' ? AGENT_PROGRESS_POLL_MS : false })
  const { data: plan, error: planError, isError: planFailed, isPaused: planPaused } = planQ
  const { data: request, error: requestError, isError: requestFailed, isPaused: requestPaused } = requestQ
  const read = useMemo(() => planRead(
    { data: plan, error: planError, isError: planFailed, isPaused: planPaused },
    { data: request, error: requestError, isError: requestFailed, isPaused: requestPaused },
    { data: progress, error: progressError, isError: progressFailed, isPaused: progressPaused },
    root ?? PLAN_ROOT,
  ), [plan, planError, planFailed, planPaused, request, requestError, requestFailed, requestPaused, progress, progressError, progressFailed, progressPaused, root])
  // A failed read says when the plan was last read whole (review 488 B1); a good one stays the same object across
  // polls, so a pane does not repaint for a read that changed nothing.
  const readAt = wholeAt(planQ.dataUpdatedAt, progressQ.dataUpdatedAt)
  return useMemo(() => (read.from === 'error' ? { ...read, readAt } : read), [read, readAt])
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
  const { data, error, isError, isPaused, dataUpdatedAt } = progressQ
  const read = useMemo(() => archRead({ data, error, isError, isPaused }), [data, error, isError, isPaused])
  const readAt = wholeAt(dataUpdatedAt)
  return useMemo(() => (read.from === 'error' ? { ...read, readAt } : read), [read, readAt])
}

/**
 * Which steps of the round a request is the approval for (v4 §20.5 scenario 10), from the same GET /plan?root= read the
 * plan panes make: read is 'ok' with the steps (none when the plan holds none, or holds no such root), or the read is not
 * done yet or failed, and nothing is said.
 */
export type ApprovalStepsRead = { read: 'ok'; stepsOf: (n: number | undefined) => ApprovalStep[] } | { read: 'loading' | 'error' }
export function useApprovalSteps(enabled = true): ApprovalStepsRead {
  const plans = useRoundPlans(enabled)
  return useMemo((): ApprovalStepsRead => {
    if (plans.read !== 'ok') return plans
    const docs = plans.docs
    return { read: 'ok', stepsOf: n => n == null || n <= 0 ? [] : docs.flatMap(doc => stepsOfApproval(doc, n)) }
  }, [plans])
}

/**
 * Every round's plan tree, one GET /plan?root= per round GET /progress counts (PLAN_ROOT alone when that read failed
 * or counts none): read is 'ok' with the trees the runtime holds, in round order; 'loading' until each has answered;
 * 'error' when any read failed, or answered a tree that is not the round asked for (review 476 C2).
 */
type RoundPlans = { read: 'ok'; docs: unknown[] } | { read: 'loading' | 'error' }
function useRoundPlans(enabled: boolean): RoundPlans {
  const progressQ = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal),
    enabled, refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const { data: progress, isError: progressFailed, isPaused: progressPaused } = progressQ
  const roundsKey = progress !== undefined ? planRounds(progress.plan).join(',') || PLAN_ROOT : progressFailed || progressPaused ? PLAN_ROOT : ''
  const rounds = useMemo(() => (roundsKey === '' ? [] : roundsKey.split(',')), [roundsKey])
  const combine = useCallback((results: UseQueryResult<PlanAnswer>[]): RoundPlans => {
    if (rounds.length === 0) return { read: 'loading' }
    if (results.some(q => q.isError || q.isPaused)) return { read: 'error' }
    if (results.some(q => q.data === undefined)) return { read: 'loading' }
    const docs: unknown[] = []
    for (const [i, q] of results.entries()) {
      if (!q.data?.found) continue
      const doc = record(q.data.doc)
      if (doc.root !== rounds[i] || !Array.isArray(doc.nodes)) return { read: 'error' }
      docs.push(q.data.doc)
    }
    return { read: 'ok', docs }
  }, [rounds])
  return useQueries({
    queries: rounds.map(root => ({ queryKey: ['agent-runtime-plan', root], queryFn: ({ signal }: { signal: AbortSignal }) => fetchRuntimePlan(root, signal),
      enabled, refetchInterval: AGENT_PROGRESS_POLL_MS, retry: false })),
    combine,
  })
}

/**
 * The steps waiting on the Owner's sign-off (the Owner 2026-10-21), from the same GET /plan?root= read the plan panes
 * make; read is 'ok' with them by number, or the read is not done yet or failed and nothing is listed. Every count of
 * them uses waits; notStarted are the steps with a sign-off rule whose work has not started, kept apart and never
 * counted (TD-425).
 */
export type SignoffWaitsRead = { read: 'ok'; waits: SignoffWait[]; notStarted: SignoffWait[] } | { read: 'loading' | 'error' }
export function useSignoffWaits(enabled = true): SignoffWaitsRead {
  const plans = useRoundPlans(enabled)
  // Every round's waits, round by round (the Owner 2026-10-22: round 3's steps waited unseen while R2 alone was read).
  return useMemo((): SignoffWaitsRead => (plans.read !== 'ok' ? plans
    : { read: 'ok', waits: plans.docs.flatMap(doc => signoffWaits(doc)), notStarted: plans.docs.flatMap(doc => signoffNotStarted(doc)) }), [plans])
}
