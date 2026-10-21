import { NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { AGENT_RUNTIME_POLL_MS, fetchRuntimeProgress } from '@/api/agentRuntime'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { mergedWaitingCount, openQuestionItem, runtimeWaiting, type WaitingItem } from './mergedNeedsYou'
import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { capacityRefusalActive, capacityRefusalText, hostsNeedingYou, silentThreads } from '@/api/agentThreads'
import { fetchApprovalList } from '@/api/approvals'
import { fetchQuestionList, type Question } from '@/api/questions'
import { useAgentThreads } from '@/hooks/useAgentThreads'
import type { NeedsYouCount } from '@/pages/shell/needs-you/needsYouModel'
import {
  groupOpenApprovals,
  mergeHandled,
  recentlyHandledApprovals,
  recentlyHandledQuestions,
  recentlyHandledThreads,
  waitingCount,
  type NeedsYouGroups,
} from '@/pages/shell/needs-you/needsYouGroups'

export const NEEDS_YOU_REFRESH_MS = 30_000

function reasonOf(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback
}

function knownOrPending(
  isError: boolean,
  error: unknown,
  data: unknown,
  fallback: string,
  count: number,
): NeedsYouCount {
  if (isError) return { state: 'unknown', reason: reasonOf(error, fallback) }
  if (data == null) return { state: 'loading' }
  return { state: 'known', count }
}

/**
 * Open approvals (decide, run in a terminal, check), open questions, and silent
 * agent threads. The waiting total is Unknown when the open list, the thread
 * list, or the question list cannot be read. A failed question refresh is
 * Unknown even when the last list is still on screen. The handled list is
 * Unknown when any list it merges (approvals `status=all`, threads,
 * questions) cannot be read (review 488 A2).
 * Running, and approved work the platform runs, are not part of the total.
 */
export function useNeedsYou() {
  const now = Date.now()
  const runtimeQuery = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), refetchInterval: AGENT_RUNTIME_POLL_MS, retry: false })
  const openQ = useQuery({
    queryKey: ['approvals', 'open'],
    queryFn: () => fetchApprovalList('open'),
    refetchInterval: NEEDS_YOU_REFRESH_MS,
    retry: false,
  })
  const allQ = useQuery({
    queryKey: ['approvals', 'all'],
    queryFn: () => fetchApprovalList('all'),
    refetchInterval: NEEDS_YOU_REFRESH_MS,
    retry: false,
  })
  const threadsQuery = useAgentThreads()
  const questionsQ = useQuery({
    queryKey: ['questions', 'all'],
    queryFn: () => fetchQuestionList('all'),
    refetchInterval: NEEDS_YOU_REFRESH_MS,
    retry: false,
  })
  const groups: NeedsYouGroups = groupOpenApprovals(openQ.data ?? [], now)
  const questions: Question[] = useMemo(() => questionsQ.data ?? [], [questionsQ.data])
  // The same list while the questions are the same: the Answer group does not repaint for another read (CI 2026-10-21).
  const openQuestions = useMemo(() => questions.filter(question => question.status === 'open'), [questions])
  // A refresh paused offline is not a read: the last list is stale, as after an error (review 351 A1).
  const questionsFailed = questionsQ.isError || questionsQ.isPaused
  const questionsUnreadable = questionsFailed && questionsQ.data == null
  const questionsLoading = questionsQ.data == null && !questionsFailed
  const questionsStale = questionsFailed && questionsQ.data != null
  const silent = silentThreads(threadsQuery.data?.threads ?? [])
  const lost = hostsNeedingYou(threadsQuery.data?.hosts)
  const capacityActive = threadsQuery.data != null && capacityRefusalActive(threadsQuery.data)
  const capacityText =
    capacityActive && threadsQuery.data?.last_thread_refusal
      ? capacityRefusalText(threadsQuery.data.threads_refused ?? 0, threadsQuery.data.last_thread_refusal)
      : ''
  const silentSectionCount = silent.length + lost.length + (capacityActive ? 1 : 0)

  const openCount = knownOrPending(
    openQ.isError || openQ.isPaused,
    openQ.isPaused ? new Error('Could not read approvals — offline.') : openQ.error,
    openQ.data,
    'approvals request failed',
    waitingCount(groups, 0),
  )
  const silentCount = knownOrPending(
    threadsQuery.isError || threadsQuery.isPaused,
    threadsQuery.isPaused ? new Error('Could not read agent threads — offline.') : threadsQuery.error,
    threadsQuery.data,
    'agent threads request failed',
    silentSectionCount,
  )

  let count: NeedsYouCount
  if (openCount.state === 'unknown') count = openCount
  else if (silentCount.state === 'unknown') count = silentCount
  else if (questionsFailed) {
    count = { state: 'unknown', reason: questionsQ.isPaused ? 'Could not read questions — offline.' : reasonOf(questionsQ.error, 'questions request failed') }
  } else if (openCount.state === 'loading' || silentCount.state === 'loading' || questionsLoading) {
    count = { state: 'loading' }
  } else count = { state: 'known', count: waitingCount(groups, silentCount.count, openQuestions.length) }

  const runtimeItems = runtimeWaiting(runtimeQuery.data?.needs_you.items ?? [], questions)
  const runtimeCount = knownOrPending(runtimeQuery.isError || runtimeQuery.isPaused || runtimeQuery.data?.needs_you.truncated === true,
    runtimeQuery.error, runtimeQuery.data, 'Agent runtime list is unavailable or truncated; count is incomplete', runtimeItems.length)
  // What waits for the Owner comes only from items with a way in: approvals, questions, the runtime.
  // The board's hand-written owner_now is not read (2.5 slice 3): the coordinator asks with a card or a request.
  count = mergedWaitingCount([count, runtimeCount])
  const waitingItems: WaitingItem[] = [
    ...(['decide', 'terminal', 'check'] as const).flatMap(group => groups[group].map(item => ({
      id: item.id, title: item.summary || item.id, href: approvalHref(item.id),
      instruction: group === 'decide' ? 'Console request · Approve or reject' : group === 'terminal' ? 'Request page · Run in a terminal and report the result' : 'Request page · Check and report what happened',
    }))),
    ...openQuestions.map(openQuestionItem),
    ...silent.map(t => ({ id: t.thread, title: t.title || t.thread, href: NEEDS_YOU_HREF, kind: 'silent_thread', instruction: 'For you · Check the silent thread' })),
    ...lost.map(h => ({ id: h.host, title: h.host, href: NEEDS_YOU_HREF, instruction: 'For you · Check the host heartbeat' })),
    ...(capacityActive ? [{ id: 'capacity', title: capacityText, href: NEEDS_YOU_HREF, instruction: 'For you · Restore thread monitoring capacity' }] : []),
    ...runtimeItems,
  ]

  const decisionItems = waitingItems.filter(item => item.kind !== 'silent_thread')
  const silentItems = waitingItems.filter(item => item.kind === 'silent_thread')

  const approvalRows = allQ.data != null ? recentlyHandledApprovals(allQ.data, now) : []
  const threadRows = threadsQuery.data != null ? recentlyHandledThreads(threadsQuery.data.threads, now) : []
  const questionRows = questionsQ.data != null ? recentlyHandledQuestions(questionsQ.data, now) : []
  const recent = mergeHandled([...approvalRows, ...threadRows, ...questionRows])

  // Handled recently merges three lists: unknown when any of them cannot be read, never a count of the others.
  let recentCount: NeedsYouCount
  if (allQ.isError || allQ.isPaused) {
    recentCount = { state: 'unknown', reason: allQ.isPaused ? 'Could not read approvals — offline.' : reasonOf(allQ.error, 'approvals request failed') }
  } else if (threadsQuery.isError || threadsQuery.isPaused) {
    recentCount = { state: 'unknown', reason: threadsQuery.isPaused ? 'Could not read agent threads — offline.' : reasonOf(threadsQuery.error, 'agent threads request failed') }
  } else if (questionsFailed) {
    recentCount = { state: 'unknown', reason: questionsQ.isPaused ? 'Could not read questions — offline.' : reasonOf(questionsQ.error, 'questions request failed') }
  } else if (allQ.data == null || threadsQuery.data == null || questionsQ.data == null) {
    recentCount = { state: 'loading' }
  } else {
    recentCount = { state: 'known', count: recent.length }
  }

  return {
    count,
    decisionItems, silentItems,
    waitingItems, runtimeItems, runtimeCount, runtimeQuery,
    openCount,
    silentCount,
    recentCount,
    decide: groups.decide,
    terminal: groups.terminal,
    check: groups.check,
    drills: groups.drills,
    recent,
    silent,
    lost,
    capacityText,
    now,
    questions,
    openQuestions,
    silentOffline: threadsQuery.isPaused && !threadsQuery.isError,
    questionsLoading,
    questionsUnreadable,
    questionsStale,
    questionsReason: !(questionsUnreadable || questionsStale) ? '' : questionsQ.isPaused ? 'offline' : reasonOf(questionsQ.error, 'questions request failed'),
    query: openQ,
    handledQuery: allQ,
    threadsQuery,
    questionsQuery: questionsQ,
  }
}
