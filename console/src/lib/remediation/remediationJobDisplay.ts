import type { RemediationJob } from '@/api/types'

export function remediationJobStatusLabel(job: RemediationJob): string {
  if (job.status === 'done') return 'Done'
  if (job.status === 'failed') return 'Failed'
  if (job.status === 'cancelled') return 'Cancelled'
  if (job.phase === 'awaiting_approval') return 'Awaiting you'
  return 'Running'
}

export function remediationScopeShortLabel(scope?: string): string {
  if (scope == null || scope === '') return 'Agent session'
  if (scope === 'cluster_issues_full_auto') return 'Cluster auto-remediate'
  if (scope === 'agent-desk') return 'Agent task'
  if (scope === 'release') return 'Release'
  if (scope === 'release-fix') return 'Release Fix'
  if (scope === 'nightly-health-check') return 'Nightly health check'
  if (scope === 'nightly-drift-autofix') return 'Drift auto-fix'
  if (scope.startsWith('cluster')) return 'Cluster'
  return scope.replace(/_/g, ' ')
}

export function remediationJobReachability(job: RemediationJob): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  if (job.phase === 'awaiting_approval') return 'degraded'
  if (job.status === 'running') return 'degraded'
  return 'unknown'
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
