import { useQuery } from '@tanstack/react-query'
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
 * Unknown when the approvals `status=all` cannot be read.
 * Running, and approved work the platform runs, are not part of the total.
 */
export function useNeedsYou() {
  const now = Date.now()
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
  const questions: Question[] = questionsQ.data ?? []
  const openQuestions = questions.filter(question => question.status === 'open')
  const questionsUnreadable = questionsQ.isError && questionsQ.data == null
  const questionsLoading = questionsQ.data == null && !questionsQ.isError
  const questionsStale = questionsQ.isError && questionsQ.data != null
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
    threadsQuery.isError,
    threadsQuery.error,
    threadsQuery.data,
    'agent threads request failed',
    silentSectionCount,
  )

  let count: NeedsYouCount
  if (openCount.state === 'unknown') count = openCount
  else if (silentCount.state === 'unknown') count = silentCount
  else if (questionsQ.isError) {
    count = { state: 'unknown', reason: reasonOf(questionsQ.error, 'questions request failed') }
  } else if (openCount.state === 'loading' || silentCount.state === 'loading' || questionsLoading) {
    count = { state: 'loading' }
  } else count = { state: 'known', count: waitingCount(groups, silentCount.count, openQuestions.length) }

  const approvalRows = allQ.data != null ? recentlyHandledApprovals(allQ.data, now) : []
  const threadRows = threadsQuery.data != null ? recentlyHandledThreads(threadsQuery.data.threads, now) : []
  const questionRows = questionsQ.data != null ? recentlyHandledQuestions(questionsQ.data, now) : []
  const recent = mergeHandled([...approvalRows, ...threadRows, ...questionRows])

  let recentCount: NeedsYouCount
  if (allQ.isError || allQ.isPaused) {
    recentCount = { state: 'unknown', reason: allQ.isPaused ? 'Could not read approvals — offline.' : reasonOf(allQ.error, 'approvals request failed') }
  } else if (allQ.data == null || (threadsQuery.data == null && !threadsQuery.isError)) {
    recentCount = { state: 'loading' }
  } else {
    recentCount = { state: 'known', count: recent.length }
  }

  return {
    count,
    openCount,
    silentCount,
    recentCount,
    decide: groups.decide,
    terminal: groups.terminal,
    check: groups.check,
    recent,
    silent,
    lost,
    capacityText,
    now,
    questions,
    openQuestions,
    questionsLoading,
    questionsUnreadable,
    questionsStale,
    questionsReason: questionsUnreadable || questionsStale ? reasonOf(questionsQ.error, 'questions request failed') : '',
    query: openQ,
    handledQuery: allQ,
    threadsQuery,
    questionsQuery: questionsQ,
  }
}
