import type { RemediationJob } from '@/api/types'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'

export function remediationJobStatusLabel(job: RemediationJob): string {
  if (job.status === 'done') return 'Done'
  if (job.status === 'failed') return 'Failed'
  if (job.status === 'cancelled') return 'Cancelled'
  if (job.phase === 'awaiting_approval') return 'Awaiting you'
  return 'Running'
}

/**
 * Display label for a job scope. Delegates to the single authoritative naming
 * table in agentTaskCatalog so the timeline, init brief and capabilities panel
 * never drift apart. Add new scope→label mappings in AGENT_TASK_CATALOG only.
 */
export function remediationScopeShortLabel(scope?: string): string {
  return scopeToLabel(scope)
}

export function remediationJobReachability(job: RemediationJob): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  if (job.phase === 'awaiting_approval') return 'degraded'
  if (job.status === 'running') return 'degraded'
  return 'unknown'
}

/** Timeline cell color — orphaned jobs are not live on the runner. */
export function remediationTimelineCellStatus(job: RemediationJob): RemediationJob['status'] {
  if (job.error === 'orphaned' || (job.status === 'running' && job.summary?.includes('Orphaned'))) {
    return 'cancelled'
  }
  return job.status
}

export function isRemediationStreamOrphanError(error: string | null | undefined): boolean {
  if (error == null || error.trim() === '') return false
  const lower = error.toLowerCase()
  return lower.includes('not found') || lower.includes('not running')
}

/** Latest remediation job that is still in progress (runner active or waiting on operator). */
export function findActiveRemediationJob(jobs: RemediationJob[]): RemediationJob | null {
  const running = jobs.filter(j => j.status === 'running')
  if (running.length === 0) return null
  const sorted = [...running].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))
  const clusterScoped = sorted.find(
    j =>
      j.scope === 'cluster_issues_full_auto' ||
      j.scope?.startsWith('cluster') === true ||
      j.scope === 'nightly-health-check',
  )
  return clusterScoped ?? sorted[0]
}

export type RemediationScopeGroup = {
  scope: string
  label: string
  jobs: RemediationJob[]
  doneCount: number
  failedCount: number
  runningCount: number
  cancelledCount: number
  /** Most recent job in the group (jobs[0]). */
  latest: RemediationJob
}

/**
 * Group jobs by scope, newest-first within each group, and order groups by
 * their most recent activity (most recently active group first).
 */
export function groupRemediationJobsByScope(
  jobs: RemediationJob[],
  perGroupLimit = 24,
): RemediationScopeGroup[] {
  const buckets = new Map<string, RemediationJob[]>()
  for (const job of jobs) {
    const key = job.scope != null && job.scope !== '' ? job.scope : 'agent-desk'
    const list = buckets.get(key)
    if (list == null) buckets.set(key, [job])
    else list.push(job)
  }

  const groups: RemediationScopeGroup[] = []
  for (const [scope, list] of buckets) {
    const sorted = [...list].sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at))
    const capped = sorted.slice(0, perGroupLimit)
    groups.push({
      scope,
      label: remediationScopeShortLabel(scope),
      jobs: capped,
      doneCount: sorted.filter(j => j.status === 'done').length,
      failedCount: sorted.filter(j => j.status === 'failed').length,
      runningCount: sorted.filter(j => remediationTimelineCellStatus(j) === 'running').length,
      cancelledCount: sorted.filter(j => remediationTimelineCellStatus(j) === 'cancelled').length,
      latest: sorted[0],
    })
  }

  return groups.sort(
    (a, b) => Date.parse(b.latest.created_at) - Date.parse(a.latest.created_at),
  )
}

export function formatRemediationJobWhen(at: string): string {
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
