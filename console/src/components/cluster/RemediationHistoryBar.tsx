import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/platform'
import type { RemediationJob } from '@/api/types'

interface RemediationHistoryBarProps {
  open: boolean
  activeJobId: string | null
  liveJobId: string | null
  onSelectJob: (job: RemediationJob) => void
  onBackToLive: () => void
}

function formatJobWhen(at: string): string {
  try {
    return new Date(at).toLocaleString(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    })
  } catch {
    return at
  }
}

function jobStatusLabel(job: RemediationJob): string {
  if (job.status === 'done') return 'Done'
  if (job.status === 'failed') return 'Failed'
  if (job.status === 'cancelled') return 'Cancelled'
  if (job.phase === 'awaiting_approval') return 'Awaiting you'
  return 'Running'
}

export function RemediationHistoryBar({
  open,
  activeJobId,
  liveJobId,
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

  const jobs = jobsQuery.data?.jobs ?? []
  const viewingHistory = liveJobId != null && activeJobId != null && activeJobId !== liveJobId

  return (
    <div className="remediation-history-bar">
      <div className="remediation-history-bar__head">
        <span className="remediation-history-bar__label">History</span>
        {viewingHistory && liveJobId != null && (
          <Button variant="outline" size="sm" className="h-6 px-2 text-[var(--text-dense-caption)]" onClick={onBackToLive}>
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
      </div>
      <div className="remediation-history-bar__list dense-scroll-x">
        {jobs.length === 0 ? (
          <span className="remediation-history-bar__empty">No past runs yet</span>
        ) : (
          jobs.slice(0, 12).map(job => {
            const selected = job.id === activeJobId
            const isLive = job.id === liveJobId && job.status === 'running'
            return (
              <button
                key={job.id}
                type="button"
                className={`remediation-history-chip${selected ? ' remediation-history-chip--active' : ''}`}
                onClick={() => onSelectJob(job)}
              >
                <span className="remediation-history-chip__id">{job.id.slice(0, 8)}</span>
                <span className={`remediation-history-chip__status remediation-history-chip__status--${job.status}`}>
                  {isLive ? 'Live' : jobStatusLabel(job)}
                </span>
                <span className="remediation-history-chip__when">{formatJobWhen(job.created_at)}</span>
              </button>
            )
          })
        )}
      </div>
    </div>
  )
}
