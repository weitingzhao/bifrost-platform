import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/remediation'
import {
  formatRemediationJobWhen,
  remediationJobReachability,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'

const RECENT_LIMIT = 5

/**
 * Compact Recent tasks for Operator Dock Agent idle state.
 * Same jobs source as Agent Desk · Observe — not a second archive UI.
 */
export function DockRecentAgentTasks({
  enabled,
  onOpenJob,
  onOpenDesk,
}: {
  /** Only poll when dock Agent idle body is visible. */
  enabled: boolean
  onOpenJob?: (jobId: string) => void
  onOpenDesk?: () => void
}) {
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    enabled,
    refetchInterval: enabled ? 30_000 : false,
    staleTime: 15_000,
  })

  const recent = useMemo(() => {
    const jobs = jobsQuery.data?.jobs ?? []
    return [...jobs]
      .sort(
        (a, b) =>
          Date.parse(b.updated_at || b.created_at) - Date.parse(a.updated_at || a.created_at),
      )
      .slice(0, RECENT_LIMIT)
  }, [jobsQuery.data?.jobs])

  return (
    <div className="console-agent-execution-dock__recent">
      <div className="console-agent-execution-dock__recent-head">
        <h3 className="console-agent-execution-dock__recent-title">Recent tasks</h3>
        {onOpenDesk != null && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            onClick={onOpenDesk}
          >
            Agent Desk
          </Button>
        )}
      </div>

      {jobsQuery.isLoading && (
        <p className="console-agent-execution-dock__recent-empty">Loading…</p>
      )}
      {jobsQuery.isError && !jobsQuery.isLoading && (
        <p className="console-agent-execution-dock__recent-empty">
          Could not load recent tasks
        </p>
      )}
      {!jobsQuery.isLoading && !jobsQuery.isError && recent.length === 0 && (
        <p className="console-agent-execution-dock__recent-empty">
          No tasks yet — start Fix from Daily Ops or Mission Launch
        </p>
      )}

      {recent.length > 0 && (
        <ul className="console-agent-execution-dock__recent-list">
          {recent.map(job => {
            const when = formatRemediationJobWhen(job.updated_at || job.created_at)
            const status = remediationJobStatusLabel(job)
            const scope = remediationScopeShortLabel(job.scope)
            const clickable = onOpenJob != null
            return (
              <li key={job.id}>
                <button
                  type="button"
                  className={cn(
                    'console-agent-execution-dock__recent-row',
                    !clickable && 'console-agent-execution-dock__recent-row--static',
                  )}
                  disabled={!clickable}
                  onClick={() => onOpenJob?.(job.id)}
                  title={`${job.id.slice(0, 8)} · ${status} · ${when}`}
                >
                  <StatusLamp value={remediationJobReachability(job)} kind="reach" />
                  <span className="console-agent-execution-dock__recent-scope">{scope}</span>
                  <span className="console-agent-execution-dock__recent-status">{status}</span>
                  <span className="console-agent-execution-dock__recent-when">{when}</span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
