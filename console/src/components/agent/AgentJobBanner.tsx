import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { RemediationJob } from '@/api/types'
import { respondRemediationJob } from '@/api/platform'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import {
  bannerStatusLabel,
  deriveAgentFeedStats,
  deriveAgentLiveFeed,
  feedKindLabel,
  formatAgentElapsed,
  formatFeedEventLine,
  recentAgentFeedEvents,
} from '@/lib/agent/agentLiveFeed'

interface AgentJobBannerProps {
  jobId: string
  /** Catalog label, e.g. "Platform · Release" */
  taskLabel?: string
  onDismiss: () => void
  /** Optional escape hatch — only inside expanded panel; never auto-navigates. */
  onOpenAgentDesk?: (jobId: string) => void
  onComplete?: (job: RemediationJob) => void
}

function reachFromPhase(job: RemediationJob | null): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job == null) return 'unknown'
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  return 'degraded'
}

export function AgentJobBanner({ jobId, taskLabel, onDismiss, onOpenAgentDesk, onComplete }: AgentJobBannerProps) {
  const qc = useQueryClient()
  const completedRef = useRef<string | null>(null)
  const autoDismissRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [feedPulse, setFeedPulse] = useState(0)

  const { job, events, connected, error } = useRemediationStream(jobId)

  const isTerminal = job?.status === 'done' || job?.status === 'failed' || job?.status === 'cancelled'
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
    mutationFn: ({ optionId, note, commitMessage }: { optionId: string; note?: string; commitMessage?: string }) =>
      respondRemediationJob(jobId, optionId, note, commitMessage),
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
    setFeedPulse(k => k + 1)
  }, [liveFeed?.text, liveFeed?.kind])

  useEffect(() => {
    if (job == null || !isTerminal || completedRef.current === job.id) return
    completedRef.current = job.id
    onComplete?.(job)
    void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
  }, [job, isTerminal, onComplete, qc])

  useEffect(() => {
    if (job?.status === 'done') {
      autoDismissRef.current = setTimeout(() => onDismiss(), 5000)
    }
    return () => {
      if (autoDismissRef.current != null) clearTimeout(autoDismissRef.current)
    }
  }, [job?.status, onDismiss])

  const bannerVariant = isTerminal
    ? job?.status === 'done'
      ? 'done'
      : 'failed'
    : isApproval
      ? 'approval'
      : 'running'

  const statusLabel = bannerStatusLabel(bannerVariant, job)
  const showInlineFeed = !isTerminal && liveFeed != null
  const showFeedPlaceholder = !isTerminal && liveFeed == null && connected
  const showStats = !isTerminal && (feedStats.toolCalls > 0 || feedStats.eventCount > 0)

  return (
    <div className={cn('agent-job-banner', `agent-job-banner--${bannerVariant}`, expanded && 'agent-job-banner--expanded')}>
      <div className="agent-job-banner__head">
        <StatusLamp value={reachFromPhase(job)} kind="reach" />
        {taskLabel != null && taskLabel !== '' && (
          <span className="agent-job-banner__task-label">{taskLabel}</span>
        )}
        <span className="agent-job-banner__label">{statusLabel}</span>
        {showInlineFeed && (
          <div className="agent-job-banner__feed" key={feedPulse}>
            <span className={cn('agent-job-banner__feed-kind', `agent-job-banner__feed-kind--${liveFeed.kind}`)}>
              {feedKindLabel(liveFeed.kind)}
            </span>
            <span className="agent-job-banner__feed-text" title={liveFeed.text}>
              {liveFeed.text}
            </span>
          </div>
        )}
        {showFeedPlaceholder && (
          <span className="agent-job-banner__feed-text agent-job-banner__feed-text--placeholder">
            Waiting for agent activity…
          </span>
        )}
        {showStats && (
          <span className="agent-job-banner__stats">
            {feedStats.toolCalls > 0 && `${feedStats.toolCalls} tool${feedStats.toolCalls === 1 ? '' : 's'}`}
            {feedStats.toolCalls > 0 && feedStats.eventCount > feedStats.toolCalls && ' · '}
            {feedStats.eventCount > feedStats.toolCalls && `${feedStats.eventCount} events`}
          </span>
        )}
        <AgentPhaseIndicator currentPhase={job?.phase} failed={job?.status === 'failed'} compact interactive />
        {elapsed != null && !isTerminal && (
          <span className="agent-job-banner__elapsed">{elapsed}</span>
        )}
        {!connected && !isTerminal && error == null && (
          <span className="agent-job-banner__connecting">connecting…</span>
        )}
        <div className="agent-job-banner__actions">
          {!isTerminal && (
            <Button
              variant="outline"
              size="xs"
              onClick={() => setExpanded(v => !v)}
              aria-expanded={expanded}
            >
              {expanded ? <ChevronUp className="agent-job-banner__action-icon" aria-hidden /> : <ChevronDown className="agent-job-banner__action-icon" aria-hidden />}
              {expanded ? 'Hide' : 'Details'}
            </Button>
          )}
          {isTerminal && (
            <Button variant="outline" size="xs" onClick={onDismiss}>
              Dismiss
            </Button>
          )}
        </div>
      </div>

      {expanded && !isTerminal && (
        <div className="agent-job-banner__detail">
          <div className="agent-job-banner__detail-log dense-scroll-y">
            {recentEvents.length === 0 ? (
              <p className="agent-job-banner__detail-empty">Waiting for agent activity…</p>
            ) : (
              <ul className="agent-job-banner__detail-list">
                {recentEvents.map(ev => (
                  <li key={ev.id} className={cn('agent-job-banner__detail-item', `agent-job-banner__detail-item--${ev.type}`)}>
                    <span className="agent-job-banner__detail-type">{ev.type}</span>
                    <span className="agent-job-banner__detail-text">{formatFeedEventLine(ev)}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {onOpenAgentDesk != null && (
            <div className="agent-job-banner__detail-footer">
              <Button variant="ghost" size="xs" onClick={() => onOpenAgentDesk(jobId)}>
                Open in Agent Desk
              </Button>
            </div>
          )}
        </div>
      )}

      {bannerVariant === 'done' && job?.summary != null && job.summary !== '' && (
        <p className="agent-job-banner__summary agent-job-banner__summary--done">{job.summary}</p>
      )}

      {bannerVariant === 'failed' && (
        <p className="agent-job-banner__summary agent-job-banner__summary--failed">
          {job?.error ?? job?.summary ?? 'Unknown error'}
        </p>
      )}

      {error != null && !isTerminal && (
        <p className="agent-job-banner__summary agent-job-banner__summary--failed">
          Connection: {error}
        </p>
      )}

      {pendingApproval != null && (
        <div className="agent-job-banner__approval">
          <RemediationApprovalBlock
            event={pendingApproval}
            compact
            submitting={respondMutation.isPending}
            onRespond={(optionId, note, commitMessage) =>
              respondMutation.mutate({ optionId, note, commitMessage })
            }
          />
        </div>
      )}
    </div>
  )
}
