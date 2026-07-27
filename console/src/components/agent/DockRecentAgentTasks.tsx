import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import {
  formatRemediationJobWhen,
  remediationJobReachability,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'

const RECENT_LIMIT = 10

function jobToAmbient(job: RemediationJob): AmbientAgentJob {
  const scope = job.scope ?? 'agent'
  return {
    id: job.id,
    scope,
    label: scopeToLabel(scope),
  }
}

/**
 * Compact Recent tasks for Operator Dock Agent slot (right rail).
 * Same jobs source as Agent Desk · Observe — not a second archive UI.
 * Click adopts the job into the dock detail pane (does not force Agent Desk tab).
 */
export function DockRecentAgentTasks({
  enabled,
  activeJobId = null,
  onSelectJob,
  onOpenDesk,
}: {
  /** Only poll when Agent tool body is visible. */
  enabled: boolean
  /** Currently shown job in the left detail pane. */
  activeJobId?: string | null
  onSelectJob?: (job: AmbientAgentJob) => void
  onOpenDesk?: () => void
}) {
  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    enabled,
    refetchInterval: enabled ? 15_000 : false,
    staleTime: 10_000,
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
    <aside className="console-agent-execution-dock__recent" aria-label="Recent agent tasks">
      <div className="console-agent-execution-dock__recent-head">
        <h3 className="console-agent-execution-dock__recent-title">Recent tasks</h3>
        {onOpenDesk != null && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            onClick={onOpenDesk}
            title="Open Agent Desk archive"
          >
            Archive
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
            const selected = activeJobId != null && activeJobId === job.id
            const clickable = onSelectJob != null
            return (
              <li key={job.id}>
                <button
                  type="button"
                  className={cn(
                    'console-agent-execution-dock__recent-row',
                    selected && 'console-agent-execution-dock__recent-row--active',
                    !clickable && 'console-agent-execution-dock__recent-row--static',
                  )}
                  disabled={!clickable}
                  aria-current={selected ? 'true' : undefined}
                  onClick={() => onSelectJob?.(jobToAmbient(job))}
                  title={`${job.id.slice(0, 8)} · ${status} · ${when}`}
                >
                  <StatusLamp value={remediationJobReachability(job)} kind="reach" />
                  <span className="console-agent-execution-dock__recent-main">
                    <span className="console-agent-execution-dock__recent-scope">{scope}</span>
                    <span className="console-agent-execution-dock__recent-meta">
                      <span className="console-agent-execution-dock__recent-status">{status}</span>
                      <span className="console-agent-execution-dock__recent-when">{when}</span>
                    </span>
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </aside>
  )
}
