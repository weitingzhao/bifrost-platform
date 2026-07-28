import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, SegmentControl, cn } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/remediation'
import type { RemediationJob } from '@/api/remediationTypes'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import {
  formatRemediationJobWhen,
  groupRemediationJobsByScope,
  remediationJobStatusLabel,
  remediationTimelineCellStatus,
} from '@/lib/remediation/remediationJobDisplay'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'

const RECENT_LIMIT = 48
const PER_GROUP = 16
const SORT_KEY = 'bifrost.console.dockRecentTasksSort.v1'

type RecentSort = 'type' | 'time'

const SORT_OPTIONS = [
  { value: 'type', label: 'Type' },
  { value: 'time', label: 'Time' },
] as const

function jobToAmbient(job: RemediationJob): AmbientAgentJob {
  const scope = job.scope ?? 'agent'
  return {
    id: job.id,
    scope,
    label: scopeToLabel(scope),
    status: job.status,
  }
}

function readStoredSort(): RecentSort {
  try {
    const raw = localStorage.getItem(SORT_KEY)
    if (raw === 'time' || raw === 'type') return raw
  } catch {
    /* ignore */
  }
  return 'type'
}

function persistSort(sort: RecentSort) {
  try {
    localStorage.setItem(SORT_KEY, sort)
  } catch {
    /* ignore */
  }
}

function TimelineCell({
  job,
  active,
  onSelect,
}: {
  job: RemediationJob
  active: boolean
  onSelect?: (job: AmbientAgentJob) => void
}) {
  const when = formatRemediationJobWhen(job.updated_at || job.created_at)
  const status = remediationJobStatusLabel(job)
  const cellStatus = remediationTimelineCellStatus(job)
  const clickable = onSelect != null
  return (
    <button
      type="button"
      title={`${job.id.slice(0, 8)} · ${status} · ${when}`}
      aria-label={`${scopeToLabel(job.scope)} ${status} ${when}`}
      aria-current={active ? 'true' : undefined}
      disabled={!clickable}
      className={cn(
        'agent-desk-timeline-cell',
        `agent-desk-timeline-cell--${cellStatus}`,
        job.phase === 'awaiting_approval' && 'agent-desk-timeline-cell--attn',
        active && 'agent-desk-timeline-cell--active',
        !clickable && 'console-agent-execution-dock__recent-cell--static',
      )}
      onClick={() => onSelect?.(jobToAmbient(job))}
    />
  )
}

/**
 * Compact Recent tasks for Operator Dock Agent slot (right rail).
 * Type view = Agent Desk timeline cells by scope; Time view = newest-first dense list.
 */
export function DockRecentAgentTasks({
  enabled,
  activeJobId = null,
  onSelectJob,
  onOpenDesk,
}: {
  enabled: boolean
  activeJobId?: string | null
  onSelectJob?: (job: AmbientAgentJob) => void
  onOpenDesk?: () => void
}) {
  const [sort, setSort] = useState<RecentSort>(readStoredSort)

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    enabled,
    refetchInterval: enabled ? 15_000 : false,
    staleTime: 10_000,
  })

  const sortedJobs = useMemo(() => {
    return [...(jobsQuery.data?.jobs ?? [])]
      .sort(
        (a, b) =>
          Date.parse(b.updated_at || b.created_at) - Date.parse(a.updated_at || a.created_at),
      )
      .slice(0, RECENT_LIMIT)
  }, [jobsQuery.data?.jobs])

  const groups = useMemo(
    () => groupRemediationJobsByScope(sortedJobs, PER_GROUP),
    [sortedJobs],
  )

  const onSortChange = (next: string) => {
    const value: RecentSort = next === 'time' ? 'time' : 'type'
    setSort(value)
    persistSort(value)
  }

  const empty = !jobsQuery.isLoading && !jobsQuery.isError && sortedJobs.length === 0

  return (
    <aside className="console-agent-execution-dock__recent" aria-label="Recent agent tasks">
      <div className="console-agent-execution-dock__recent-head">
        <h3 className="console-agent-execution-dock__recent-title">Recent</h3>
        <SegmentControl
          ariaLabel="Recent tasks sort"
          options={[...SORT_OPTIONS]}
          value={sort}
          onChange={onSortChange}
          size="xs"
          className="console-agent-execution-dock__recent-sort"
        />
        {onOpenDesk != null && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="ml-auto text-[var(--text-dense-caption)] text-muted-foreground"
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
        <p className="console-agent-execution-dock__recent-empty">Could not load recent tasks</p>
      )}
      {empty && (
        <p className="console-agent-execution-dock__recent-empty">
          No tasks yet — start Fix from Daily Ops or Mission Launch
        </p>
      )}

      {!empty && sort === 'type' && (
        <div className="console-agent-execution-dock__recent-groups dense-scroll-y">
          {groups.map(group => (
            <div key={group.scope} className="console-agent-execution-dock__recent-group">
              <div className="console-agent-execution-dock__recent-group-head">
                <span
                  className="console-agent-execution-dock__recent-group-label"
                  title={group.scope}
                >
                  {group.label}
                </span>
                <span className="console-agent-execution-dock__recent-group-counts">
                  {group.runningCount > 0 && (
                    <span className="agent-desk-timeline-count agent-desk-timeline-count--running">
                      {group.runningCount}
                    </span>
                  )}
                  {group.doneCount > 0 && (
                    <span className="agent-desk-timeline-count agent-desk-timeline-count--done">
                      {group.doneCount}
                    </span>
                  )}
                  {group.failedCount > 0 && (
                    <span className="agent-desk-timeline-count agent-desk-timeline-count--failed">
                      {group.failedCount}
                    </span>
                  )}
                  {group.cancelledCount > 0 && (
                    <span className="agent-desk-timeline-count agent-desk-timeline-count--cancelled">
                      {group.cancelledCount}
                    </span>
                  )}
                </span>
              </div>
              <div className="agent-desk-timeline-track console-agent-execution-dock__recent-track">
                <span className="agent-desk-timeline-track__now">now</span>
                {group.jobs.map(job => (
                  <TimelineCell
                    key={job.id}
                    job={job}
                    active={activeJobId != null && activeJobId === job.id}
                    onSelect={onSelectJob}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {!empty && sort === 'time' && (
        <ul className="console-agent-execution-dock__recent-chrono dense-scroll-y">
          {sortedJobs.map(job => {
            const when = formatRemediationJobWhen(job.updated_at || job.created_at)
            const status = remediationJobStatusLabel(job)
            const cellStatus = remediationTimelineCellStatus(job)
            const scope = scopeToLabel(job.scope)
            const selected = activeJobId != null && activeJobId === job.id
            const clickable = onSelectJob != null
            return (
              <li key={job.id}>
                <button
                  type="button"
                  className={cn(
                    'console-agent-execution-dock__recent-chrono-row',
                    selected && 'console-agent-execution-dock__recent-chrono-row--active',
                    !clickable && 'console-agent-execution-dock__recent-chrono-row--static',
                  )}
                  disabled={!clickable}
                  aria-current={selected ? 'true' : undefined}
                  title={`${job.id.slice(0, 8)} · ${status} · ${when}`}
                  onClick={() => onSelectJob?.(jobToAmbient(job))}
                >
                  <span
                    className={cn(
                      'agent-desk-timeline-cell',
                      `agent-desk-timeline-cell--${cellStatus}`,
                      job.phase === 'awaiting_approval' && 'agent-desk-timeline-cell--attn',
                      'console-agent-execution-dock__recent-chrono-swatch',
                    )}
                    aria-hidden
                  />
                  <span className="console-agent-execution-dock__recent-chrono-main">
                    <span className="console-agent-execution-dock__recent-chrono-scope">{scope}</span>
                    <span className="console-agent-execution-dock__recent-chrono-when">{when}</span>
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
