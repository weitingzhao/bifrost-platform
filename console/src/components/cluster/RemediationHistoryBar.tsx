import { useMemo } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/platform'
import type { RemediationJob } from '@/api/types'
import {
  formatRemediationJobWhen,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
  remediationTimelineCellStatus,
} from '@/lib/remediation/remediationJobDisplay'

interface RemediationHistoryBarProps {
  open: boolean
  activeJobId: string | null
  liveJobId: string | null
  /** Scope of the current task — history is filtered to this scope only. */
  scope?: string | null
  onSelectJob: (job: RemediationJob) => void
  onBackToLive: () => void
}

export function RemediationHistoryBar({
  open,
  activeJobId,
  liveJobId,
  scope,
  onSelectJob,
  onBackToLive,
}: RemediationHistoryBarProps) {
  const qc = useQueryClient()
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    enabled: open,
    refetchInterval: open ? 15_000 : false,
  })

  const allJobs = jobsQuery.data?.jobs ?? []

  const jobs = useMemo(() => {
    const scopeKey = scope != null && scope !== '' ? scope : null
    const filtered =
      scopeKey != null
        ? allJobs.filter(j => (j.scope != null && j.scope !== '' ? j.scope : 'agent-desk') === scopeKey)
        : allJobs
    return [...filtered]
      .sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))
      .slice(0, 24)
  }, [allJobs, scope])

  const counts = useMemo(() => {
    let done = 0
    let failed = 0
    let running = 0
    let cancelled = 0
    for (const job of jobs) {
      const status = remediationTimelineCellStatus(job)
      if (job.status === 'done') done++
      else if (job.status === 'failed') failed++
      else if (status === 'running') running++
      else if (status === 'cancelled') cancelled++
    }
    return { done, failed, running, cancelled }
  }, [jobs])

  const viewingHistory = liveJobId != null && activeJobId != null && activeJobId !== liveJobId
  const scopeLabel = scope != null && scope !== '' ? remediationScopeShortLabel(scope) : 'All tasks'

  return (
    <div className="remediation-history-bar">
      <div className="remediation-history-bar__head">
        <span className="remediation-history-bar__label">History</span>
        <span className="remediation-history-bar__scope">{scopeLabel}</span>
        <span className="remediation-history-bar__counts">
          {counts.done > 0 && (
            <span className="agent-desk-timeline-count agent-desk-timeline-count--done">{counts.done} ok</span>
          )}
          {counts.failed > 0 && (
            <span className="agent-desk-timeline-count agent-desk-timeline-count--failed">{counts.failed} failed</span>
          )}
          {counts.running > 0 && (
            <span className="agent-desk-timeline-count agent-desk-timeline-count--running">{counts.running} running</span>
          )}
          {counts.cancelled > 0 && (
            <span className="agent-desk-timeline-count agent-desk-timeline-count--cancelled">
              {counts.cancelled} cancelled
            </span>
          )}
        </span>
        <span className="remediation-history-bar__actions">
          {viewingHistory && liveJobId != null && (
            <Button
              variant="outline"
              size="sm"
              className="h-6 px-2 text-[var(--text-dense-caption)]"
              onClick={onBackToLive}
            >
              Back to live
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-[var(--text-dense-caption)]"
            disabled={jobsQuery.isFetching}
            onClick={() => void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })}
          >
            {jobsQuery.isFetching ? '…' : 'Refresh'}
          </Button>
        </span>
      </div>
      {jobs.length === 0 ? (
        <span className="remediation-history-bar__empty">No past runs yet</span>
      ) : (
        <div className="agent-desk-timeline-track remediation-history-bar__track">
          <span className="agent-desk-timeline-track__now">now</span>
          {jobs.map(job => {
            const isLive = job.id === liveJobId && job.status === 'running'
            return (
              <button
                key={job.id}
                type="button"
                title={`${job.id.slice(0, 8)} · ${isLive ? 'Live' : remediationJobStatusLabel(job)} · ${formatRemediationJobWhen(job.created_at)}`}
                aria-label={`${scopeLabel} ${isLive ? 'Live' : remediationJobStatusLabel(job)} ${formatRemediationJobWhen(job.created_at)}`}
                className={[
                  'agent-desk-timeline-cell',
                  `agent-desk-timeline-cell--${remediationTimelineCellStatus(job)}`,
                  job.phase === 'awaiting_approval' ? ' agent-desk-timeline-cell--attn' : '',
                  job.id === activeJobId ? ' agent-desk-timeline-cell--active' : '',
                ].join(' ')}
                onClick={() => onSelectJob(job)}
              />
            )
          })}
        </div>
      )}
    </div>
  )
}
