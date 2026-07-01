import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronRight, History } from 'lucide-react'
import { fetchRemediationJobs } from '@/api/platform'
import type { RemediationJob } from '@/api/types'
import {
  findActiveRemediationJobs,
  findRecentCompletedRemediationJobs,
  formatRemediationJobWhen,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'

interface ActiveAgentJobsStripProps {
  onOpenAgentDesk?: (jobId: string) => void
  onOpenAudit?: () => void
}

function ActiveJobChip({
  job,
  onClick,
  variant = 'live',
}: {
  job: RemediationJob
  onClick: () => void
  variant?: 'live' | 'recent'
}) {
  const awaiting = job.phase === 'awaiting_approval'
  const label = remediationScopeShortLabel(job.scope)
  const status = remediationJobStatusLabel(job)

  return (
    <button
      type="button"
      className={[
        'control-room-active-job',
        variant === 'recent' ? 'control-room-active-job--recent' : '',
        awaiting ? 'control-room-active-job--awaiting' : '',
      ].join(' ')}
      onClick={onClick}
      title={`${label} · ${status} · ${formatRemediationJobWhen(job.updated_at)}`}
    >
      <Bot size={13} className="control-room-active-job__icon" />
      <span className="control-room-active-job__scope">{label}</span>
      <span className="control-room-active-job__status">{status}</span>
      {variant === 'live' && job.summary != null && job.summary !== '' && (
        <span className="control-room-active-job__summary">{job.summary}</span>
      )}
      <ChevronRight size={12} className="control-room-active-job__chevron" />
    </button>
  )
}

export function ActiveAgentJobsStrip({ onOpenAgentDesk, onOpenAudit }: ActiveAgentJobsStripProps) {
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 10_000,
  })

  const allJobs = jobsQuery.data?.jobs ?? []
  const activeJobs = findActiveRemediationJobs(allJobs)
  const recentJobs = findRecentCompletedRemediationJobs(allJobs)

  return (
    <section className="control-room-active-jobs" aria-label="Active Agent jobs">
      <div className="control-room-active-jobs__head">
        <Bot size={14} className="control-room-active-jobs__head-icon" />
        <span className="control-room-active-jobs__title">Agent loop</span>
        {activeJobs.length > 0 && (
          <span className="control-room-active-jobs__count">{activeJobs.length} live</span>
        )}
        {onOpenAudit != null && (
          <button type="button" className="control-room-active-jobs__audit" onClick={onOpenAudit}>
            <History size={12} />
            Audit
          </button>
        )}
      </div>
      {jobsQuery.isLoading ? (
        <p className="control-room-active-jobs__empty">Loading Agent jobs…</p>
      ) : activeJobs.length === 0 && recentJobs.length === 0 ? (
        <p className="control-room-active-jobs__empty">No active Agent jobs — runner idle.</p>
      ) : (
        <div className="control-room-active-jobs__sections">
          {activeJobs.length > 0 && (
            <div className="control-room-active-jobs__list">
              {activeJobs.map(job => (
                <ActiveJobChip
                  key={job.id}
                  job={job}
                  variant="live"
                  onClick={() => onOpenAgentDesk?.(job.id)}
                />
              ))}
            </div>
          )}
          {recentJobs.length > 0 && (
            <div className="control-room-active-jobs__recent">
              <span className="control-room-active-jobs__recent-label">Recent</span>
              <div className="control-room-active-jobs__list">
                {recentJobs.map(job => (
                  <ActiveJobChip
                    key={job.id}
                    job={job}
                    variant="recent"
                    onClick={() => onOpenAgentDesk?.(job.id)}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </section>
  )
}
