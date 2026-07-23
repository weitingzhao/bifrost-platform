import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import type { RemediationEvent, RemediationJob } from '@/api/remediationTypes'
import { respondRemediationJob } from '@/api/remediation'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import {
  bannerStatusLabel,
  deriveAgentFeedStats,
  deriveAgentLiveFeed,
  formatAgentElapsed,
  recentAgentFeedEvents,
  type AgentFeedStats,
  type AgentLiveFeed,
} from '@/lib/agent/agentLiveFeed'

export type AgentJobBannerVariant = 'running' | 'approval' | 'done' | 'failed'

export type AgentJobLiveSession = {
  job: RemediationJob | null
  events: RemediationEvent[]
  connected: boolean
  error: string | null
  isTerminal: boolean
  isApproval: boolean
  pendingApproval: RemediationEvent | null
  liveFeed: AgentLiveFeed | null
  feedStats: AgentFeedStats
  recentEvents: RemediationEvent[]
  elapsed: string | null
  bannerVariant: AgentJobBannerVariant
  statusLabel: string
  respondPending: boolean
  respondError: Error | null
  respond: (optionId: string, note?: string, commitMessage?: string) => void
  reach: 'ok' | 'degraded' | 'fail' | 'unknown'
}

function reachFromPhase(job: RemediationJob | null): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job == null) return 'unknown'
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  return 'degraded'
}

/**
 * Shared live session for ambient Agent Fix UI (Execution Dock + legacy banner).
 * Single stream consumer + approval respond path — avoid a third feed parser.
 */
export function useAgentJobLiveSession(
  jobId: string,
  opts?: {
    onComplete?: (job: RemediationJob) => void
    onDismiss?: () => void
    /** Auto-dismiss after success (ms). Default 5000; set 0 to disable. */
    autoDismissMs?: number
  },
): AgentJobLiveSession {
  const qc = useQueryClient()
  const completedRef = useRef<string | null>(null)
  const autoDismissRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const onCompleteRef = useRef(opts?.onComplete)
  const onDismissRef = useRef(opts?.onDismiss)
  onCompleteRef.current = opts?.onComplete
  onDismissRef.current = opts?.onDismiss
  const autoDismissMs = opts?.autoDismissMs ?? 5000

  const [nowMs, setNowMs] = useState(() => Date.now())

  const { job, events, connected, error } = useRemediationStream(jobId)

  const isTerminal =
    job?.status === 'done' || job?.status === 'failed' || job?.status === 'cancelled'
  const isApproval = job?.phase === 'awaiting_approval' && !isTerminal

  const pendingApproval = useMemo(() => {
    if (!isApproval) return null
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i]
      if (ev.type === 'approval_request') return ev
      if (ev.type === 'status' && ev.text.startsWith('Operator selected:')) return null
    }
    return null
  }, [events, isApproval])

  const liveFeed = useMemo(() => deriveAgentLiveFeed(events), [events])
  const feedStats = useMemo(() => deriveAgentFeedStats(events), [events])
  const recentEvents = useMemo(() => recentAgentFeedEvents(events), [events])
  const elapsed = formatAgentElapsed(job?.created_at, nowMs)

  const respondMutation = useMutation({
    mutationFn: ({
      optionId,
      note,
      commitMessage,
    }: {
      optionId: string
      note?: string
      commitMessage?: string
    }) => respondRemediationJob(jobId, optionId, note, commitMessage),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })

  useEffect(() => {
    if (isTerminal) return
    const id = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [isTerminal])

  useEffect(() => {
    if (job == null || !isTerminal || completedRef.current === job.id) return
    completedRef.current = job.id
    onCompleteRef.current?.(job)
    void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
  }, [job, isTerminal, qc])

  useEffect(() => {
    if (job?.status === 'done' && autoDismissMs > 0 && onDismissRef.current != null) {
      autoDismissRef.current = setTimeout(() => onDismissRef.current?.(), autoDismissMs)
    }
    return () => {
      if (autoDismissRef.current != null) clearTimeout(autoDismissRef.current)
    }
  }, [job?.status, autoDismissMs])

  const bannerVariant: AgentJobBannerVariant = isTerminal
    ? job?.status === 'done'
      ? 'done'
      : 'failed'
    : isApproval
      ? 'approval'
      : 'running'

  return {
    job,
    events,
    connected,
    error,
    isTerminal,
    isApproval,
    pendingApproval,
    liveFeed,
    feedStats,
    recentEvents,
    elapsed,
    bannerVariant,
    statusLabel: bannerStatusLabel(bannerVariant, job),
    respondPending: respondMutation.isPending,
    respondError: (respondMutation.error as Error | null) ?? null,
    respond: (optionId, note, commitMessage) =>
      respondMutation.mutate({ optionId, note, commitMessage }),
    reach: reachFromPhase(job),
  }
}
