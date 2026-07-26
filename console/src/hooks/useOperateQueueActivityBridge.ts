import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchRemediationJobs } from '@/api/remediation'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import {
  getActivityEvents,
  upsertActivity,
  updateActivityPhase,
} from '@/lib/activity/activityStore'

function isTerminalJobStatus(status: string | undefined): boolean {
  return status === 'done' || status === 'failed' || status === 'cancelled'
}

/**
 * Surface Operate Queue handoffs that have an execution_job_id (Agent-handoff only).
 * Reconciles against remediation job terminal status so Activity does not stay
 * "applying" / spinning after the Agent job finished.
 */
export function useOperateQueueActivityBridge(): void {
  const query = useOperateQueue()
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 30_000,
    staleTime: 15_000,
  })

  useEffect(() => {
    if (query.data == null) return

    const open = query.data.open ?? []
    const closed = query.data.recent_closed ?? []
    const jobsLoaded = jobsQuery.isSuccess
    const jobs = jobsQuery.data?.jobs ?? []
    const jobById = new Map(jobs.map(j => [j.id, j]))

    const openJobIds = new Set<string>()
    for (const item of open) {
      const jobId = item.execution_job_id
      if (jobId == null || jobId === '') continue
      openJobIds.add(jobId)
      const job = jobById.get(jobId)
      const terminal = jobsLoaded && job != null && isTerminalJobStatus(job.status)
      const failed = terminal && job != null && job.status === 'failed'
      const phase = terminal ? (failed ? 'failed' : 'completed') : 'applying'
      const prev = getActivityEvents().find(e => e.id === `agent:queue:${jobId}`)
      upsertActivity({
        id: `agent:queue:${jobId}`,
        kind: 'agent',
        phase,
        title: item.title,
        target: jobId,
        detail: terminal
          ? (job?.summary ?? job?.error ?? `Job ${job?.status}`)
          : 'Operate queue execution',
        settledOutcome: terminal ? (failed ? 'error' : 'resolved') : undefined,
        linkTo: 'agent-desk',
        bumpTs: prev == null || prev.phase !== phase,
      })
    }

    for (const item of closed) {
      const jobId = item.execution_job_id
      if (jobId == null || jobId === '') continue
      const id = `agent:queue:${jobId}`
      const job = jobById.get(jobId)
      const failed = job?.status === 'failed'
      updateActivityPhase(id, failed ? 'failed' : 'completed', {
        settledOutcome: failed ? 'error' : 'resolved',
        detail: 'Operate queue item closed',
        title: item.title,
      })
    }

    // Session-persisted applying rows: settle when job is terminal or left open queue.
    for (const ev of getActivityEvents()) {
      if (!ev.id.startsWith('agent:queue:')) continue
      if (ev.phase === 'completed' || ev.phase === 'failed' || ev.phase === 'settled') continue
      const jobId = ev.target ?? ev.id.slice('agent:queue:'.length)
      if (jobId === '') continue

      const job = jobById.get(jobId)
      if (jobsLoaded && job != null && isTerminalJobStatus(job.status)) {
        updateActivityPhase(ev.id, job.status === 'failed' ? 'failed' : 'completed', {
          settledOutcome: job.status === 'failed' ? 'error' : 'resolved',
          detail: job.summary ?? job.error ?? `Job ${job.status}`,
        })
        continue
      }

      if (!openJobIds.has(jobId) && jobsLoaded && job == null) {
        updateActivityPhase(ev.id, 'completed', {
          settledOutcome: 'resolved',
          detail: 'Operate queue item no longer open',
        })
      }
    }
  }, [query.data, jobsQuery.data, jobsQuery.isSuccess])
}
