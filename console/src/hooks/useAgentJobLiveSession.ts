import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { RemediationEvent, RemediationJob } from '@/api/remediationTypes'
import { fetchRemediationJob, respondRemediationJob } from '@/api/remediation'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import {
  bannerStatusLabel,
  deriveAgentFeedStats,
  deriveAgentLiveFeed,
  dockAgentFeedEvents,
  formatAgentElapsed,
  type AgentFeedStats,
  type AgentLiveFeed,
} from '@/lib/agent/agentLiveFeed'
import { isRemediationStreamOrphanError } from '@/lib/remediation/remediationJobDisplay'

export type AgentJobBannerVariant = 'running' | 'approval' | 'done' | 'failed'

export type AgentJobLiveSession = {
  job: RemediationJob | null
  events: RemediationEvent[]
  connected: boolean
  error: string | null
  isTerminal: boolean
  isApproval: boolean
  /** True when live stream is gone and we are reading archive snapshot. */
  isArchive: boolean
  /** Snapshot GET in flight (history still loading). */
  historyLoading: boolean
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

function isTerminalStatus(job: RemediationJob | null | undefined): boolean {
  return job?.status === 'done' || job?.status === 'failed' || job?.status === 'cancelled'
}

/** Union events by id, preserve chronological order (created order / at). */
function mergeEventsById(...lists: Array<RemediationEvent[] | undefined>): RemediationEvent[] {
  const byID = new Map<string, RemediationEvent>()
  const order: string[] = []
  for (const list of lists) {
    if (list == null) continue
    for (const ev of list) {
      if (ev.id === '') continue
      if (!byID.has(ev.id)) order.push(ev.id)
      byID.set(ev.id, ev)
    }
  }
  return order.map(id => byID.get(id)!).sort((a, b) => {
    const ta = Date.parse(a.at)
    const tb = Date.parse(b.at)
    if (Number.isFinite(ta) && Number.isFinite(tb) && ta !== tb) return ta - tb
    return 0
  })
}

/**
 * Shared live session for ambient Agent Fix UI (Execution Dock + legacy banner).
 *
 * Always loads GET /remediation/:id snapshot so Recent tasks (running or historical)
 * show interaction history. Live SSE is additive for in-flight jobs.
 */
export function useAgentJobLiveSession(
  jobId: string | null,
  opts?: {
    onComplete?: (job: RemediationJob) => void
    onDismiss?: () => void
    /** Auto-dismiss after success (ms). Default 5000; set 0 to disable. */
    autoDismissMs?: number
    /**
     * Hint from Recent list — skip opening live stream for known terminal jobs.
     * Still loads archive snapshot for detail / events.
     */
    knownTerminal?: boolean
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
  const knownTerminal = opts?.knownTerminal === true

  const [nowMs, setNowMs] = useState(() => Date.now())
  const [streamOrphan, setStreamOrphan] = useState(false)

  useEffect(() => {
    setStreamOrphan(false)
    completedRef.current = null
  }, [jobId])

  const streamEnabled =
    jobId != null && jobId !== '' && !streamOrphan && !knownTerminal

  const { job: streamJob, events: liveEvents, connected, error: streamError } =
    useRemediationStream(streamEnabled ? jobId : null)

  useEffect(() => {
    if (streamError == null) return
    if (!isRemediationStreamOrphanError(streamError)) return
    setStreamOrphan(true)
  }, [streamError])

  /** Always load snapshot — history must work for running and finished tasks. */
  const snapshotQuery = useQuery({
    queryKey: ['remediation', 'job', jobId],
    queryFn: () => fetchRemediationJob(jobId!),
    enabled: jobId != null && jobId !== '',
    staleTime: 5_000,
    refetchInterval: streamEnabled && connected ? 15_000 : false,
  })

  const listHint = useMemo(() => {
    if (jobId == null || jobId === '') return null
    const cached = qc.getQueryData<{ jobs: RemediationJob[] }>(['remediation', 'jobs'])
    return cached?.jobs?.find(j => j.id === jobId) ?? null
  }, [jobId, qc])

  const baseJob: RemediationJob | null =
    streamJob ?? snapshotQuery.data ?? listHint ?? null

  const isArchive =
    knownTerminal ||
    streamOrphan ||
    (baseJob != null && isTerminalStatus(baseJob) && !connected)

  const job: RemediationJob | null = useMemo(() => {
    if (baseJob == null) return null
    if (!streamOrphan && baseJob.error !== 'orphaned') return baseJob
    if (isTerminalStatus(baseJob) && baseJob.error !== 'orphaned') return baseJob
    return {
      ...baseJob,
      status: baseJob.status === 'running' ? 'cancelled' : baseJob.status,
      phase: baseJob.status === 'running' ? 'cancelled' : baseJob.phase,
      error: baseJob.error === 'orphaned' ? 'orphaned' : baseJob.error,
      summary:
        baseJob.summary ||
        (baseJob.status === 'running'
          ? 'Job lost contact with the remediation runner (stale "running" state).'
          : baseJob.summary),
    }
  }, [baseJob, streamOrphan])

  const events: RemediationEvent[] = useMemo(
    () =>
      mergeEventsById(
        listHint?.events,
        snapshotQuery.data?.events,
        streamJob?.events,
        liveEvents,
      ),
    [listHint?.events, snapshotQuery.data?.events, streamJob?.events, liveEvents],
  )

  const isTerminal = isTerminalStatus(job)
  const isApproval = job?.phase === 'awaiting_approval' && !isTerminal
  const historyLoading =
    jobId != null &&
    jobId !== '' &&
    snapshotQuery.isLoading &&
    events.length === 0 &&
    job == null

  /** Hide stream 404 once archive/snapshot (or list hint) is available. */
  const error =
    isArchive && (snapshotQuery.data != null || listHint != null || isTerminalStatus(job))
      ? null
      : streamOrphan && snapshotQuery.isError
        ? snapshotQuery.error instanceof Error
          ? snapshotQuery.error.message
          : 'Archive job unavailable'
        : streamError

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
  const recentEvents = useMemo(() => dockAgentFeedEvents(events), [events])
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
    }) => {
      if (jobId == null || jobId === '') {
        return Promise.reject(new Error('No ambient agent job'))
      }
      return respondRemediationJob(jobId, optionId, note, commitMessage)
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['remediation', 'job', jobId] })
    },
  })

  useEffect(() => {
    if (jobId == null || jobId === '' || isTerminal) return
    const id = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [jobId, isTerminal])

  useEffect(() => {
    if (job == null || !isTerminal || completedRef.current === job.id) return
    // Browse an already-finished job from Recent — do not re-fire complete handlers.
    // Live jobs that finish into archive (stream orphan) must still settle Activity.
    if (knownTerminal) {
      completedRef.current = job.id
      return
    }
    completedRef.current = job.id
    onCompleteRef.current?.(job)
    void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
  }, [job, isTerminal, qc, knownTerminal])

  useEffect(() => {
    if (job?.status === 'done' && autoDismissMs > 0 && onDismissRef.current != null) {
      if (knownTerminal || isArchive) return
      autoDismissRef.current = setTimeout(() => onDismissRef.current?.(), autoDismissMs)
    }
    return () => {
      if (autoDismissRef.current != null) clearTimeout(autoDismissRef.current)
    }
  }, [job?.status, autoDismissMs, knownTerminal, isArchive])

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
    connected: connected && !isArchive,
    error,
    isTerminal,
    isApproval,
    isArchive,
    historyLoading,
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
