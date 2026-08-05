import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchRemediationJobs } from '@/api/remediation'
import {
  getActivityEvents,
  updateActivityPhase,
} from '@/lib/activity/activityStore'

function isTerminalJobStatus(status: string | undefined): boolean {
  return status === 'done' || status === 'failed' || status === 'cancelled'
}

/**
 * Reconcile shell Activity rows `agent:<jobId>` (ambient Fix / Operator Dock)
 * against remediation job list so APPLYING does not stick after the job finished
 * (e.g. live stream orphaned into archive before onComplete, or user switched Recent).
 */
export function useAgentActivityBridge(): void {
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: () => fetchRemediationJobs(),
    refetchInterval: 15_000,
    staleTime: 8_000,
  })

  useEffect(() => {
    if (!jobsQuery.isSuccess) return
    const jobs = jobsQuery.data?.jobs ?? []
    const byId = new Map(jobs.map(j => [j.id, j]))

    for (const ev of getActivityEvents()) {
      if (!ev.id.startsWith('agent:')) continue
      if (ev.id.startsWith('agent:queue:')) continue
      if (ev.phase === 'completed' || ev.phase === 'failed' || ev.phase === 'settled') continue

      const jobId = ev.id.slice('agent:'.length)
      if (jobId === '') continue
      const job = byId.get(jobId)
      if (job == null || !isTerminalJobStatus(job.status)) continue

      const failed = job.status === 'failed' || job.status === 'cancelled'
      updateActivityPhase(ev.id, failed ? 'failed' : 'completed', {
        settledOutcome: failed ? 'error' : 'resolved',
        detail: job.summary?.trim() || job.error?.trim() || job.status,
      })
    }
  }, [jobsQuery.isSuccess, jobsQuery.dataUpdatedAt, jobsQuery.data])
}
