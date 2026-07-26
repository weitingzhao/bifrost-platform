import { useEffect, useRef } from 'react'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { upsertActivity, updateActivityPhase } from '@/lib/activity/activityStore'

/**
 * Surface Operate Queue handoffs that have an execution_job_id (Agent-handoff only).
 * Does not inflate the queue with restarts — read-only bridge into Activity Feed.
 */
export function useOperateQueueActivityBridge(): void {
  const query = useOperateQueue()
  const seen = useRef(new Set<string>())

  useEffect(() => {
    const open = query.data?.open ?? []
    for (const item of open) {
      const jobId = item.execution_job_id
      if (jobId == null || jobId === '') continue
      const id = `agent:queue:${jobId}`
      if (seen.current.has(id)) continue
      seen.current.add(id)
      upsertActivity({
        id,
        kind: 'agent',
        phase: 'applying',
        title: item.title,
        target: jobId,
        detail: 'Operate queue execution',
        linkTo: 'agent-desk',
        bumpTs: true,
      })
    }

    const closed = query.data?.recent_closed ?? []
    for (const item of closed) {
      const jobId = item.execution_job_id
      if (jobId == null || jobId === '') continue
      const id = `agent:queue:${jobId}`
      if (!seen.current.has(id)) {
        seen.current.add(id)
        continue
      }
      updateActivityPhase(id, 'completed', {
        settledOutcome: 'resolved',
        detail: 'Operate queue item closed',
      })
    }
  }, [query.data])
}
