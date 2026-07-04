import { useEffect, useMemo, useRef } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, StatusLamp } from '@bifrost/ui'
import type { RemediationJob } from '@/api/types'
import { respondRemediationJob } from '@/api/platform'
import { AgentPhaseIndicator, PHASE_STEPS } from '@/components/agent/AgentPhaseIndicator'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { useRemediationStream } from '@/hooks/useRemediationStream'

interface AgentJobBannerProps {
  jobId: string
  onDismiss: () => void
  onViewDetails?: (jobId: string) => void
  onComplete?: (job: RemediationJob) => void
}

function reachFromPhase(job: RemediationJob | null): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job == null) return 'unknown'
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  return 'degraded'
}

function phaseLabel(job: RemediationJob | null): string {
  if (job == null) return 'Starting…'
  const step = PHASE_STEPS.find(s => s.key === job.phase)
  return step?.label ?? job.phase ?? 'Running'
}

export function AgentJobBanner({ jobId, onDismiss, onViewDetails, onComplete }: AgentJobBannerProps) {
  const qc = useQueryClient()
  const completedRef = useRef<string | null>(null)
  const autoDismissRef = useRef<ReturnType<typeof setTimeout> | null>(null)

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

  const respondMutation = useMutation({
    mutationFn: ({ optionId, note, commitMessage }: { optionId: string; note?: string; commitMessage?: string }) =>
      respondRemediationJob(jobId, optionId, note, commitMessage),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })

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

  return (
    <div className={`agent-job-banner agent-job-banner--${bannerVariant}`}>
      <div className="agent-job-banner__head">
        <StatusLamp value={reachFromPhase(job)} kind="reach" />
        <span className="agent-job-banner__label">
          {bannerVariant === 'done'
            ? 'Agent completed'
            : bannerVariant === 'failed'
              ? 'Agent failed'
              : bannerVariant === 'approval'
                ? 'Agent needs your decision'
                : `Agent fixing… ${phaseLabel(job)}`}
        </span>
        <AgentPhaseIndicator currentPhase={job?.phase} failed={job?.status === 'failed'} compact />
        {!connected && !isTerminal && error == null && (
          <span className="agent-job-banner__connecting">connecting…</span>
        )}
        <div className="agent-job-banner__actions">
          {onViewDetails != null && (
            <Button variant="outline" size="xs" onClick={() => onViewDetails(jobId)}>
              View details
            </Button>
          )}
          <Button variant="outline" size="xs" onClick={onDismiss}>
            {isTerminal ? 'Dismiss' : 'Hide'}
          </Button>
        </div>
      </div>

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
